/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package node replays "kubeadm join" and kubelet node registration in-process so a mock DPU shows
// up as a Ready Node in the DPU cluster without kubeadm, kubelet or any admin credentials. The DPU
// cluster sees the same traffic as with a real kubelet: bootstrap token authentication, a CSR for
// the kube-apiserver-client-kubelet signer, a Node object and Lease heartbeats.
package node

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	provcertificate "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/bootstrap"

	certificates "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/certificate"
	"k8s.io/klog/v2"
)

const (
	pairNamePrefix          = "kubelet-client"
	kubeconfigFile          = "node-kubeconfig"
	bootstrapKubeconfigFile = "node-bootstrap-kubeconfig"
	pkiDir                  = "node-pki"
	clusterInfoNamespace    = "kube-public"
	clusterInfoName         = "cluster-info"
	discoveryTimeout        = 30 * time.Second
	certWaitInterval        = 5 * time.Second
)

// JoinCommand is the parsed "kubeadm join" line the controller stores in the <dpu>-kubeadm-join
// Secret.
type JoinCommand struct {
	Server       string
	Token        string
	CACertHashes []string
}

var (
	tokenRe = regexp.MustCompile(`--token[= ]+([a-z0-9]{6}\.[a-z0-9]{16})`)
	hashRe  = regexp.MustCompile(`--discovery-token-ca-cert-hash[= ]+(sha256:[0-9a-fA-F]{64})`)
)

// ParseJoinCommand extracts server, token and CA hashes from a kubeadm join command line.
func ParseJoinCommand(cmd string) (*JoinCommand, error) {
	fields := strings.Fields(cmd)
	if len(fields) < 3 || fields[0] != "kubeadm" || fields[1] != "join" {
		return nil, fmt.Errorf("not a kubeadm join command: %q", cmd)
	}
	jc := &JoinCommand{Server: fields[2]}
	if strings.HasPrefix(jc.Server, "--") {
		return nil, fmt.Errorf("kubeadm join command has no API server address: %q", cmd)
	}
	m := tokenRe.FindStringSubmatch(cmd)
	if m == nil {
		return nil, fmt.Errorf("kubeadm join command has no --token: %q", cmd)
	}
	jc.Token = m[1]
	for _, h := range hashRe.FindAllStringSubmatch(cmd, -1) {
		jc.CACertHashes = append(jc.CACertHashes, strings.ToLower(h[1]))
	}
	if len(jc.CACertHashes) == 0 {
		return nil, fmt.Errorf("kubeadm join command has no --discovery-token-ca-cert-hash: %q", cmd)
	}
	return jc, nil
}

// DiscoverCA performs kubeadm token discovery: it reads the kube-public/cluster-info ConfigMap
// anonymously (TLS unverified), takes the cluster CA from the embedded kubeconfig and pins it
// against the SPKI hashes of the join command. The JWS signature check kubeadm also performs is
// omitted; the hash pin is what protects against a spoofed API server.
func DiscoverCA(ctx context.Context, server string, hashes []string) ([]byte, error) {
	cfg := &restclient.Config{
		Host:            "https://" + server,
		TLSClientConfig: restclient.TLSClientConfig{Insecure: true},
		Timeout:         discoveryTimeout,
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("anonymous discovery client: %w", err)
	}
	cm, err := cs.CoreV1().ConfigMaps(clusterInfoNamespace).Get(ctx, clusterInfoName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read %s/%s from %s: %w", clusterInfoNamespace, clusterInfoName, server, err)
	}
	kubeconfig, err := clientcmd.Load([]byte(cm.Data["kubeconfig"]))
	if err != nil {
		return nil, fmt.Errorf("parse cluster-info kubeconfig: %w", err)
	}
	var caData []byte
	for _, c := range kubeconfig.Clusters {
		caData = c.CertificateAuthorityData
		break
	}
	if len(caData) == 0 {
		return nil, fmt.Errorf("cluster-info kubeconfig carries no CA certificate")
	}
	certs, err := certutil.ParseCertsPEM(caData)
	if err != nil {
		return nil, fmt.Errorf("parse cluster CA: %w", err)
	}
	want := map[string]struct{}{}
	for _, h := range hashes {
		want[strings.ToLower(h)] = struct{}{}
	}
	for _, c := range certs {
		sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
		if _, ok := want["sha256:"+hex.EncodeToString(sum[:])]; ok {
			return caData, nil
		}
	}
	return nil, fmt.Errorf("cluster CA does not match any --discovery-token-ca-cert-hash of the join command")
}

// Joiner holds one mock kubelet's DPU cluster credentials. Files live under dir so a restarted
// process (VM mode) or a rebooted agent run reuses the node certificate instead of the token.
// EnsureCredentials is not safe for concurrent calls; the agent runs of one DPU are sequential.
type Joiner struct {
	dir      string
	nodeName string

	cfg       *restclient.Config
	clientset kubernetes.Interface
	stop      func()
}

// NewJoiner returns a joiner for nodeName whose files live under dir.
func NewJoiner(dir, nodeName string) *Joiner {
	return &Joiner{dir: dir, nodeName: nodeName}
}

// NodeName returns the DPU cluster Node name.
func (j *Joiner) NodeName() string { return j.nodeName }

// Clientset returns the node-identity client, valid after EnsureCredentials.
func (j *Joiner) Clientset() kubernetes.Interface { return j.clientset }

// EnsureCredentials makes the node client certificate available. joinCmd is the current kubeadm
// join command, or "" when the controller has already deleted the join Secret; in that case the
// certificate obtained by an earlier run must still be on disk. Certificate rotation runs until
// ctx is canceled.
func (j *Joiner) EnsureCredentials(ctx context.Context, joinCmd string) error {
	if err := os.MkdirAll(filepath.Join(j.dir, pkiDir), 0o700); err != nil {
		return err
	}
	kubeconfigPath := filepath.Join(j.dir, kubeconfigFile)
	bootstrapPath := filepath.Join(j.dir, bootstrapKubeconfigFile)
	if joinCmd != "" {
		jc, err := ParseJoinCommand(joinCmd)
		if err != nil {
			return err
		}
		caData, err := DiscoverCA(ctx, jc.Server, jc.CACertHashes)
		if err != nil {
			return err
		}
		if err := writeBootstrapKubeconfig(bootstrapPath, jc.Server, jc.Token, caData); err != nil {
			return err
		}
	} else if _, err := os.Stat(bootstrapPath); os.IsNotExist(err) {
		if _, err := os.Stat(kubeconfigPath); err != nil {
			return fmt.Errorf("no kubeadm join command and no previous node credentials under %s", j.dir)
		}
		bootstrapPath = ""
	}

	certConfig, clientConfig, err := bootstrap.LoadClientConfig(kubeconfigPath, bootstrapPath, filepath.Join(j.dir, pkiDir), pairNamePrefix)
	if err != nil {
		return fmt.Errorf("load node client config: %w", err)
	}
	store, err := certificate.NewFileStore(pairNamePrefix, filepath.Join(j.dir, pkiDir), filepath.Join(j.dir, pkiDir), clientConfig.CertFile, clientConfig.KeyFile)
	if err != nil {
		return fmt.Errorf("node certificate store: %w", err)
	}
	mgr, err := certificate.NewManager(&certificate.Config{
		ClientsetFn: func(current *tls.Certificate) (kubernetes.Interface, error) {
			cfg := certConfig
			if current != nil {
				cfg = clientConfig
			}
			return kubernetes.NewForConfig(cfg)
		},
		Template: &x509.CertificateRequest{Subject: pkix.Name{
			CommonName:   "system:node:" + j.nodeName,
			Organization: []string{"system:nodes"},
		}},
		// The kubelet signer is what kubeadm's node-autoapprove-bootstrap RBAC lets the
		// kube-controller-manager approve without human intervention.
		SignerName:       certificates.KubeAPIServerClientKubeletSignerName,
		GetUsages:        certificate.DefaultKubeletClientGetUsages,
		CertificateStore: store,
	})
	if err != nil {
		return fmt.Errorf("node certificate manager: %w", err)
	}

	if j.stop != nil {
		j.stop()
	}
	stopCh := make(chan struct{})
	transportConfig := restclient.AnonymousClientConfig(clientConfig)
	closeConns, err := provcertificate.UpdateTransport(stopCh, transportConfig, mgr, 0)
	if err != nil {
		close(stopCh)
		return fmt.Errorf("node transport: %w", err)
	}
	mgr.Start()
	// stop releases this run's resources exactly once. It is reached from the next
	// EnsureCredentials call and from the ctx goroutine below, in either order or concurrently.
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(stopCh)
			mgr.Stop()
			closeConns()
		})
	}
	j.stop = stop
	// The goroutine uses this run's stop, not j.stop: j.stop may already belong to the next run
	// when ctx is canceled, and that run must not be torn down here.
	go func() {
		<-ctx.Done()
		stop()
	}()

	klog.InfoS("waiting for DPU cluster node client certificate", "node", j.nodeName)
	if err := wait.PollUntilContextCancel(ctx, certWaitInterval, true, func(context.Context) (bool, error) {
		return mgr.Current() != nil, nil
	}); err != nil {
		return fmt.Errorf("wait for node client certificate: %w", err)
	}
	cs, err := kubernetes.NewForConfig(transportConfig)
	if err != nil {
		return err
	}
	j.cfg = transportConfig
	j.clientset = cs
	klog.InfoS("DPU cluster node identity ready", "node", j.nodeName, "server", transportConfig.Host)
	return nil
}

func writeBootstrapKubeconfig(path, server, token string, caData []byte) error {
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"dpu-cluster": {
			Server:                   "https://" + server,
			CertificateAuthorityData: caData,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"kubelet-bootstrap": {Token: token}},
		Contexts: map[string]*clientcmdapi.Context{"bootstrap": {
			Cluster:  "dpu-cluster",
			AuthInfo: "kubelet-bootstrap",
		}},
		CurrentContext: "bootstrap",
	}
	return clientcmd.WriteToFile(cfg, path)
}

// ServerVersion returns the DPU cluster API server version; the mock reports it as kubeletVersion.
func (j *Joiner) ServerVersion() (string, error) {
	if j.clientset == nil {
		return "", fmt.Errorf("node credentials not ready")
	}
	v, err := j.clientset.Discovery().ServerVersion()
	if err != nil {
		return "", err
	}
	return v.GitVersion, nil
}
