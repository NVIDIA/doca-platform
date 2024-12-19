/*
Copyright 2024 NVIDIA

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

package state

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/bfcfg"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	"github.com/openconfig/gnmi/proto/gnmi"
	ospb "github.com/openconfig/gnoi/os"
	"github.com/openconfig/gnoi/system"
	tpb "github.com/openconfig/gnoi/types"
	"github.com/openconfig/gnoigo"
	gos "github.com/openconfig/gnoigo/os"
	gsystem "github.com/openconfig/gnoigo/system"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	TemplateFile            = "bf.cfg.template"
	CloudInitDefaultTimeout = 90
	// The maximum size of the bf.cfg file is expanded to 128k since DOCA 2.8
	MaxBFSize     = 1024 * 128
	MaxRetryCount = 10
	// HostNameDPULabelKey is the label added to the DPU Kubernetes Node that indicates the hostname of the host that
	// this DPU belongs to.
	HostNameDPULabelKey = "provisioning.dpu.nvidia.com/host"
)

type dpuOSInstallingState struct {
	dpu *provisioningv1.DPU
}

func (st *dpuOSInstallingState) Handle(ctx context.Context, client client.Client, _ dutil.DPUOptions) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	dmsTaskName := generateDMSTaskName(st.dpu)
	state := st.dpu.Status.DeepCopy()

	// check whether bfb exist
	nn := types.NamespacedName{
		Namespace: st.dpu.Namespace,
		Name:      st.dpu.Spec.BFB,
	}
	bfb := &provisioningv1.BFB{}
	if err := client.Get(ctx, nn, bfb); err != nil {
		if apierrors.IsNotFound(err) {
			// BFB does not exist, dpu keeps in installing state.
			logger.V(3).Info(fmt.Sprintf("BFB %v is not ready for DMS %v", st.dpu.Spec.BFB, dmsTaskName))
			return *state, nil
		}
		return *state, err
	}

	if dmsTask, ok := dutil.OsInstallTaskMap.Load(dmsTaskName); ok {
		result := dmsTask.(*future.Future)
		if result.GetState() == future.Ready {
			dutil.OsInstallTaskMap.Delete(dmsTaskName)
			if _, err := result.GetResult(); err == nil {
				logger.V(3).Info(fmt.Sprintf("DMS task %v is finished", dmsTaskName))
				return updateState(state, provisioningv1.DPURebooting, "DPU is rebooting"), nil
			} else {
				logger.V(3).Info(fmt.Sprintf("DMS task %v is failed with err: %v", dmsTaskName, err))
				return updateState(state, provisioningv1.DPUError, err.Error()), err
			}
		} else {
			logger.V(3).Info(fmt.Sprintf("DMS task %v is being processed", dmsTaskName))
		}
	} else {
		dmsHandler(ctx, client, st.dpu, bfb)
	}

	return *state, nil
}

func updateState(state *provisioningv1.DPUStatus, phase provisioningv1.DPUPhase, message string) provisioningv1.DPUStatus {
	state.Phase = phase
	cond := cutil.DPUCondition(provisioningv1.DPUCondOSInstalled, "", message)
	if phase == provisioningv1.DPUError {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "InstallationFailed"
	}
	cutil.SetDPUCondition(state, cond)
	return *state
}

func createGRPCConnection(ctx context.Context, client client.Client, dpu *provisioningv1.DPU) (*grpc.ClientConn, error) {
	nn := types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      cutil.GenerateDMSPodName(dpu.Name),
	}
	pod := &corev1.Pod{}
	if err := client.Get(ctx, nn, pod); err != nil {
		return nil, fmt.Errorf("failed to get pod: %v", err)
	}

	nn = types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      dms.DMSClientSecret,
	}
	dmsClientSecret := &corev1.Secret{}
	if err := client.Get(ctx, nn, dmsClientSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("client secret not found: %v", err)
		} else {
			return nil, fmt.Errorf("failed to get client secret: %v", err)
		}
	}

	// Extract the certificate and key from the secret
	dmsClientCert, certOk := dmsClientSecret.Data["tls.crt"]
	if !certOk {
		return nil, fmt.Errorf("tls.crt not found in client secret")
	}
	dmsClientKey, keyOk := dmsClientSecret.Data["tls.key"]
	if !keyOk {
		return nil, fmt.Errorf("tls.key not found in client secret")
	}

	// Load the DMS client's certificate and private key
	clientCert, err := tls.X509KeyPair(dmsClientCert, dmsClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert and key: %v", err)
	}

	// Retrieve the DMS Server secret
	dmsServerSecret := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: cutil.GenerateDMSServerSecretName(dpu.Name)}, dmsServerSecret); err != nil {
		return nil, fmt.Errorf("get server secret: %v", err)
	}

	// Extract the CA certificate from the Server secret
	serverCACert, caCertOk := dmsServerSecret.Data["ca.crt"]
	if !caCertOk {
		return nil, fmt.Errorf("ca.crt not found in Server secret")
	}

	// Create a certificate pool and add the CA certificate
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(serverCACert) {
		return nil, fmt.Errorf("failed to append Server certificate")
	}

	// Create a mTLS config with the client certificate and CA certificate
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
	}

	serverAddress := dms.Address(pod.Status.PodIP)

	// Create a gRPC connection using grpc.NewClient
	conn, err := grpc.NewClient(serverAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %v", err)
	}

	return conn, nil
}

func dmsHandler(ctx context.Context, k8sClient client.Client, dpu *provisioningv1.DPU, bfb *provisioningv1.BFB) {
	dmsTaskName := generateDMSTaskName(dpu)
	dmsTask := future.New(func() (any, error) {
		logger := log.FromContext(ctx)
		logger.V(3).Info(fmt.Sprintf("DMS %s start os installation", dmsTaskName))

		conn, err := createGRPCConnection(ctx, k8sClient, dpu)
		if err != nil {
			logger.Error(err, fmt.Sprintf("Error creating gRPC connection: %v", err))
			return future.Ready, err
		}
		defer conn.Close() //nolint: errcheck
		gnoiClient := gnoigo.NewClients(conn)
		gnmiClient := gnmi.NewGNMIClient(conn)

		modeRequest := setModeRequest()
		if resp, err := gnmiClient.Set(ctx, modeRequest); err == nil {
			logger.V(3).Info(fmt.Sprintf("Set DPU mode to DPU %s successfully, %v", dpu.Name, resp.String()))
		} else {
			logger.Error(err, "failed set DPU mode", "DPU", dpu.Name)
			return future.Ready, err
		}

		logger.V(3).Info(fmt.Sprintf("TLS Connection established between DPU controller to DMS %s", dmsTaskName))

		osInstall := gos.NewInstallOperation()
		osInstall.Version(bfb.Spec.FileName)
		// Open the file at the specified path
		fullFileName := cutil.GenerateBFBFilePath(bfb.Spec.FileName)
		bfbfile, err := os.Open(fullFileName)
		if err != nil {
			logger.Error(err, "failed to open file", "name", bfb.Spec.FileName)
			return nil, err
		}
		defer bfbfile.Close() //nolint: errcheck
		osInstall.Reader(bfbfile)
		if response, err := gnoigo.Execute(ctx, gnoiClient, osInstall); err != nil {
			return nil, err
		} else {
			logger.V(3).Info(fmt.Sprintf("DMS %s Performed InstallOperation %v", dmsTaskName, response))
		}

		flavor := &provisioningv1.DPUFlavor{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: dpu.Namespace,
			Name:      dpu.Spec.DPUFlavor,
		}, flavor); err != nil {
			return nil, err
		}
		dc := &provisioningv1.DPUCluster{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: dpu.Spec.Cluster.Namespace, Name: dpu.Spec.Cluster.Name}, dc); err != nil {
			return "", fmt.Errorf("failed to get DPUCluster, err: %v", err)
		}
		node := &corev1.Node{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: "",
			Name:      dpu.Spec.NodeName,
		}, node); err != nil {
			return nil, err
		}
		data, err := generateBFConfig(ctx, dpu, node, flavor, dc)
		if err != nil || data == nil {
			logger.Error(err, fmt.Sprintf("failed bf.cfg creation for %s/%s", dpu.Namespace, dpu.Name))
			return nil, err
		}
		cfgInstall := gos.NewInstallOperation()
		// DMS has a default rule: bf cfg file must end with .cfg
		// cfgVersion is the BF CFG file name used in DMS
		cfgVersion := cutil.GenerateBFCFGFileName(dpu.Name)

		// Make sure there is no old bf cfg file in the shared volume
		cfgFile := cutil.GenerateBFBCFGFilePath(cfgVersion)
		if err := os.Remove(cfgFile); err != nil && !os.IsNotExist(err) {
			msg := fmt.Sprintf("Delete old BFB CFG file %s failed", cfgFile)
			logger.Error(err, msg)
			return nil, err
		}

		cfgInstall.Version(cfgVersion)
		cfgInstall.Reader(bytes.NewReader(data))
		if response, err := gnoigo.Execute(ctx, gnoiClient, cfgInstall); err != nil {
			return nil, err
		} else {
			logger.V(3).Info(fmt.Sprintf("DMS %s Performed BF configuration %v", dmsTaskName, response))
		}

		bfbPathInDms := dms.DMSImageFolder + string(filepath.Separator) + bfb.Spec.FileName
		if md5, _, err := computeBFBMD5InDms(dpu.Namespace, cutil.GenerateDMSPodName(dpu.Name), "", bfbPathInDms); err == nil {
			logger.V(3).Info(fmt.Sprintf("md5sum of %s in DMS is %s", bfb.Spec.FileName, md5))
		} else {
			logger.Error(err, "Failed to get md5sum from dms", "md5", dpu)
		}

		activateOp := gos.NewActivateOperation()
		noReboot := false
		activateOp.NoReboot(noReboot)
		activateOp.Version(fmt.Sprintf("%s;%s", bfb.Spec.FileName, cfgVersion))

		logger.V(3).Info("Starting execute activate operation")
		// set 30 minutes timeout for OS installation
		activateCtx, cancel := context.WithTimeout(context.Background(), 30*60*time.Second)
		defer cancel()
		var response *ospb.ActivateResponse
		for retry := 1; retry <= MaxRetryCount; retry++ {
			response, err = gnoigo.Execute(activateCtx, gnoiClient, activateOp)
			logger.V(3).Info(fmt.Sprintf("DMS task %s is finished", dmsTaskName))
			if err == nil {
				break
			} else {
				logger.Error(err, fmt.Sprintf("DMS %s failed to Execute activateOp, retry %d", dmsTaskName, retry))
			}
		}
		if err != nil {
			return nil, err
		}

		logger.V(3).Info("Performed activateOp", "response", response)

		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-activateCtx.Done():
				logger.Error(activateCtx.Err(), fmt.Sprintf("DMS %s RebootStatusOperation timed out or was canceled", dmsTaskName))
				return nil, activateCtx.Err()
			case <-ticker.C:
				rebootStatusOp := gsystem.NewRebootStatusOperation().Subcomponents([]*tpb.Path{{Elem: []*tpb.PathElem{{Name: "CPU"}}}})
				response, err := gnoigo.Execute(activateCtx, gnoiClient, rebootStatusOp)
				if err != nil {
					logger.Error(err, fmt.Sprintf("DMS %s failed to Execute rebootStatusOp", dmsTaskName))
					return nil, err
				}
				if !response.Active {
					// Once the reboot is no longer active, check the status
					switch response.Status.Status {
					case system.RebootStatus_STATUS_SUCCESS:

						// TODO: wait for 90 sec for cloud-init
						cloudInitTimeout := CloudInitDefaultTimeout
						if value, exists := os.LookupEnv("CLOUD_INIT_TIMEOUT"); exists {
							if cloudInitTimeout, err = strconv.Atoi(value); err != nil {
								cloudInitTimeout = CloudInitDefaultTimeout
							}
						}

						time.Sleep(time.Duration(cloudInitTimeout) * time.Second)
						logger.V(3).Info(fmt.Sprintf("DMS %s install DPU successfully", dmsTaskName))

						/*verifyOp := gos.NewVerifyOperation()
						if response, err := gnoigo.Execute(ctx, clients, verifyOp); err != nil {
							logger.Error(err, fmt.Sprintf("DMS %s failed to Execute verifyOp", dmsTaskName))
							return future.Ready, err
						} else {
							logger.V(3).Info("Performed verifyOp", "response", response)
							if response.Version != fmt.Sprintf("%s;%s", bfb.Spec.FileName, cfgVersion) {
								return future.Ready, fmt.Errorf("verifyOp failure: expected: %s, received: %s", fmt.Sprintf("%s;%s", bfb.Spec.FileName, cfgVersion), response.Version)
							}
						}
						*/

						if dpu.Spec.Cluster.NodeLabels == nil {
							dpu.Spec.Cluster.NodeLabels = make(map[string]string)
						}

						// Add DOCA_BFB version to DPU NodeLabels as it is found in DPU ARM under /etc/mlnx-release. e.g.,
						// cat /etc/mlnx-release
						// bf-bundle-2.7.0-33_24.04_ubuntu-22.04_prod
						patch := client.MergeFrom(dpu.DeepCopy())
						dpu.Spec.Cluster.NodeLabels["provisioning.dpu.nvidia.com/DOCA-BFB-version"] = cutil.GenerateBFBVersionFromURL(bfb.Spec.URL)
						dpu.Spec.Cluster.NodeLabels[HostNameDPULabelKey] = dpu.Spec.NodeName
						if err := k8sClient.Patch(ctx, dpu, patch); err != nil {
							return future.Ready, err
						}
						return nil, nil
					case system.RebootStatus_STATUS_RETRIABLE_FAILURE:
						logger.Error(fmt.Errorf("DMS %s reboot encountered a retriable failure: %s", dmsTaskName, response.Reason), fmt.Sprintf("DMS %s reboot failed", dmsTaskName))
						return nil, fmt.Errorf("retriable failure: %s", response.Reason)
					case system.RebootStatus_STATUS_FAILURE:
						logger.Error(fmt.Errorf("reboot failure: %s", response.Reason), fmt.Sprintf("DMS %s reboot failed", dmsTaskName))
						return nil, fmt.Errorf("reboot failure: %s", response.Reason)
					default:
						logger.Error(fmt.Errorf("unknown reboot status: %s", response.Status), fmt.Sprintf("DMS %s encountered an unknown reboot status", dmsTaskName))
						return nil, fmt.Errorf("unknown reboot status: %s", response.Status)
					}
				}
			}
		}
	})
	dutil.OsInstallTaskMap.Store(dmsTaskName, dmsTask)
}

func generateDMSTaskName(dpu *provisioningv1.DPU) string {
	return fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name)
}

func generateBFConfig(ctx context.Context, dpu *provisioningv1.DPU, node *corev1.Node, flavor *provisioningv1.DPUFlavor, dc *provisioningv1.DPUCluster) ([]byte, error) {
	logger := log.FromContext(ctx)

	joinCommand, err := generateJoinCommand(dc)
	if err != nil {
		return nil, err
	}

	additionalReboot := false
	cmd, _, err := reboot.GenerateCmd(node.Annotations, dpu.Annotations)
	if err != nil {
		logger.Error(err, "failed to generate ipmitool command")
		return nil, err
	}

	if cmd == reboot.Skip {
		additionalReboot = true
	}

	buf, err := bfcfg.Generate(flavor, cutil.GenerateNodeName(dpu), joinCommand, additionalReboot)
	if err != nil {
		return nil, err
	}
	if buf == nil {
		return nil, fmt.Errorf("failed bf.cfg creation due to buffer issue")
	}
	if len(buf) > MaxBFSize {
		return nil, fmt.Errorf("bf.cfg for %s size (%d) exceeds the maximum limit (%d)", dpu.Name, len(buf), MaxBFSize)
	}
	logger.V(3).Info(fmt.Sprintf("bf.cfg for %s has len: %d data: %s", dpu.Name, len(buf), string(buf)))

	return buf, nil
}

func generateJoinCommand(dc *provisioningv1.DPUCluster) (string, error) {
	fp := cutil.AdminKubeConfigPath(*dc)
	if _, err := os.Stat(fp); err != nil {
		return "", fmt.Errorf("failed to stat kubeconfig file, err: %v", err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("kubeadm", "token", "create", "--print-join-command", fmt.Sprintf("--kubeconfig=%s", fp))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to run \"kubeadm token create\", err: %v, stderr: %s", err, stderr.String())
	}
	joinCommand := strings.TrimRight(stdout.String(), "\r\n") + " --v=5"
	return joinCommand, nil
}

func computeBFBMD5InDms(ns, name, container, filepath string) (string, string, error) {
	md5CMD := fmt.Sprintf("md5sum %s", filepath)
	return cutil.RemoteExec(ns, name, container, md5CMD)
}

func setModeRequest() *gnmi.SetRequest {
	return &gnmi.SetRequest{
		Update: []*gnmi.Update{
			{
				Path: &gnmi.Path{
					Elem: []*gnmi.PathElem{
						{Name: "nvidia"},
						{Name: "mode"},
						{Name: "config"},
						{Name: "mode"},
					},
				},
				Val: &gnmi.TypedValue{
					Value: &gnmi.TypedValue_StringVal{StringVal: "DPU"},
				},
			},
		},
	}
}
