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

package sosreport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/internal/digest"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// labelManagedBy is the label key for identifying resources managed by dpfctl.
	labelManagedBy = "app.kubernetes.io/managed-by"
	// labelComponent is the label key for identifying the component.
	labelComponent = "dpfctl.dpu.nvidia.com/component"
	// labelCaseID is the label key for the case ID.
	labelCaseID = "dpfctl.dpu.nvidia.com/case-id"
	// labelNodeID is the label key for a bounded node identifier.
	labelNodeID = "dpfctl.dpu.nvidia.com/node-id"
	// annotationNode is the annotation key for the node name.
	annotationNode = "dpfctl.dpu.nvidia.com/node"
	// annotationCluster is the annotation key for the cluster name.
	annotationCluster = "dpfctl.dpu.nvidia.com/cluster"
	// componentValue is the component label value for sosreport resources.
	componentValue = "sosreport"
	// managedByValue is the managed-by label value.
	managedByValue = "dpfctl"

	outputDir      = "/sos-output"
	secretDataKey  = "kubeconfig"
	secretBaseName = "sos-admin-config"

	generatedNameSuffixLength    = 5
	maxJobGenerateNamePrefixSize = validation.LabelValueMaxLength - generatedNameSuffixLength
	hashLength                   = 10
)

// OutputMode defines where the SOS report output is stored.
type OutputMode string

const (
	OutputLocal OutputMode = "local"
	OutputNFS   OutputMode = "nfs"
)

// JobOptions contains the configuration for creating a SOS report Job.
type JobOptions struct {
	Namespace   string
	NodeName    string
	CaseID      string
	Image       string
	ClusterName string
	Timeout     time.Duration
	Output      OutputMode
	NFSServer   string
	NFSPath     string
	NFSSubDir   string
	NFSUID      int64
	Archive     bool
	ArchiveOnly bool
}

// commonLabels returns the standard labels for all sosreport resources.
func commonLabels(caseID string) map[string]string {
	return map[string]string{
		labelManagedBy: managedByValue,
		labelComponent: componentValue,
		labelCaseID:    caseID,
	}
}

// podLabels returns labels for node-specific sosreport resources.
func podLabels(caseID, nodeName string) map[string]string {
	labels := commonLabels(caseID)
	labels[labelNodeID] = nodeLabelValue(nodeName)
	return labels
}

// commonAnnotations returns the standard annotations for all sosreport resources.
func commonAnnotations(clusterName string) map[string]string {
	return map[string]string{
		annotationCluster: clusterName,
	}
}

// jobAnnotations returns annotations for node-specific sosreport resources.
func jobAnnotations(clusterName, nodeName string) map[string]string {
	annotations := commonAnnotations(clusterName)
	annotations[annotationNode] = nodeName
	return annotations
}

// selectorLabels returns labels used to select sosreport resources.
func selectorLabels() map[string]string {
	return map[string]string{
		labelManagedBy: managedByValue,
		labelComponent: componentValue,
	}
}

// jobGenerateName returns a Kubernetes GenerateName prefix for a node/case pair.
// The final Job name is propagated by Kubernetes into the pod template's
// batch.kubernetes.io/job-name label, so the generated name must stay within
// the 63-byte label value limit. The readable part uses the short node name,
// while the hash uses the full node name to disambiguate FQDNs with the same
// first label.
func jobGenerateName(caseID, nodeName string) string {
	rawName := fmt.Sprintf("sos-%s-%s", caseID, nodeName)
	name := dns1123NamePart(fmt.Sprintf("sos-%s-%s", caseID, shortNodeName(nodeName)))
	hash := shortHash(rawName)
	if len(name)+len(hash)+2 <= maxJobGenerateNamePrefixSize {
		return fmt.Sprintf("%s-%s-", name, hash)
	}

	name = truncateDNS1123NamePart(name, maxJobGenerateNamePrefixSize-len(hash)-2)
	return fmt.Sprintf("%s-%s-", name, hash)
}

// secretName returns the kubeconfig Secret name for a given cluster.
func secretName(clusterName string) string {
	return fmt.Sprintf("%s-%s", secretBaseName, clusterName)
}

// CreateJob creates a SOS report Job on the given cluster for the specified node.
func CreateJob(ctx context.Context, c client.Client, opts JobOptions) (*batchv1.Job, error) {
	sosContainer := corev1.Container{
		Name:            "sosreport",
		Image:           opts.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{Name: "CASE_ID", Value: opts.CaseID},
			{Name: "OUTPUT_DIR", Value: outputDir},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
			RunAsUser:  ptr.To(int64(0)),
		},
		VolumeMounts: append(baseVolumeMounts(), corev1.VolumeMount{
			MountPath: outputDir, Name: "output",
		}),
	}

	var container corev1.Container
	volumes := baseVolumes(secretName(opts.ClusterName))

	// Sosreport always runs as an init container. The pod entering Running phase
	// means sosreport has completed — giving us a single readiness check for both modes.
	initContainers := []corev1.Container{sosContainer}

	switch opts.Output {
	case OutputNFS:
		// NFS with root_squash: sosreport must run as root (privileged host access),
		// but root can't write to NFS. Strategy:
		//   1. mkdir   (NFS UID) — create subdir on NFS
		//   2. sosreport (root)  — write to staging emptyDir
		//   3. copy    (NFS UID) — copy from staging to NFS
		const stagingDir = "/sos-staging"

		// Sosreport writes to the staging emptyDir instead of the NFS volume.
		for i := range initContainers {
			if initContainers[i].Name != "sosreport" {
				continue
			}
			for j := range initContainers[i].Env {
				if initContainers[i].Env[j].Name == "OUTPUT_DIR" {
					initContainers[i].Env[j].Value = stagingDir
				}
			}
			initContainers[i].VolumeMounts = append(baseVolumeMounts(),
				corev1.VolumeMount{MountPath: stagingDir, Name: "staging"},
			)
		}

		nfsTarget := outputDir
		if opts.NFSSubDir != "" {
			nfsTarget = outputDir + "/" + opts.NFSSubDir
		}

		if opts.ArchiveOnly && opts.NFSSubDir != "" {
			// Archive-only: tar staging directly to NFS — no mkdir or copy needed.
			archiveName := opts.NFSSubDir + ".tar.gz"
			initContainers = append(initContainers, corev1.Container{
				Name:    "archive",
				Image:   opts.Image,
				Command: []string{"tar", "czf", outputDir + "/" + archiveName, "-C", stagingDir, "."},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To(opts.NFSUID),
				},
				VolumeMounts: []corev1.VolumeMount{
					{MountPath: stagingDir, Name: "staging", ReadOnly: true},
					{MountPath: outputDir, Name: "output"},
				},
			})
		} else {
			// Copy reports to NFS, optionally creating a subdirectory first.
			if opts.NFSSubDir != "" {
				initContainers = append([]corev1.Container{{
					Name:    "mkdir",
					Image:   opts.Image,
					Command: []string{"mkdir", "-p", nfsTarget},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: ptr.To(opts.NFSUID),
					},
					VolumeMounts: []corev1.VolumeMount{
						{MountPath: outputDir, Name: "output"},
					},
				}}, initContainers...)
			}

			initContainers = append(initContainers, corev1.Container{
				Name:    "copy-to-nfs",
				Image:   opts.Image,
				Command: []string{"sh", "-c", fmt.Sprintf("cp -a %s/. %s/", stagingDir, nfsTarget)},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To(opts.NFSUID),
				},
				VolumeMounts: []corev1.VolumeMount{
					{MountPath: stagingDir, Name: "staging", ReadOnly: true},
					{MountPath: outputDir, Name: "output"},
				},
			})

			// Optionally also create an archive on NFS alongside the files.
			if opts.Archive && opts.NFSSubDir != "" {
				archiveName := opts.NFSSubDir + ".tar.gz"
				initContainers = append(initContainers, corev1.Container{
					Name:    "archive",
					Image:   opts.Image,
					Command: []string{"tar", "czf", outputDir + "/" + archiveName, "-C", outputDir, opts.NFSSubDir},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: ptr.To(opts.NFSUID),
					},
					VolumeMounts: []corev1.VolumeMount{
						{MountPath: outputDir, Name: "output"},
					},
				})
			}
		}

		// Main container just exits — Job completes after all init containers finish.
		container = corev1.Container{
			Name:    "done",
			Image:   opts.Image,
			Command: []string{"echo", "SOS report written to NFS"},
		}
		volumes = append(volumes,
			corev1.Volume{
				Name: "staging",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
			corev1.Volume{
				Name: "output",
				VolumeSource: corev1.VolumeSource{
					NFS: &corev1.NFSVolumeSource{
						Server: opts.NFSServer,
						Path:   opts.NFSPath,
					},
				},
			},
		)
	case OutputLocal:
		// Main container sleeps so we can download the report from the pod.
		container = corev1.Container{
			Name:    "sleep",
			Image:   opts.Image,
			Command: []string{"sleep", "infinity"},
			VolumeMounts: []corev1.VolumeMount{
				{MountPath: outputDir, Name: "output"},
			},
		}
		volumes = append(volumes, corev1.Volume{
			Name: "output",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	labels := podLabels(opts.CaseID, opts.NodeName)
	annotations := jobAnnotations(opts.ClusterName, opts.NodeName)
	generateName := jobGenerateName(opts.CaseID, opts.NodeName)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName,
			Namespace:    opts.Namespace,
			Labels:       labels,
			Annotations:  annotations,
		},
		Spec: batchv1.JobSpec{
			Completions:           ptr.To(int32(1)),
			Parallelism:           ptr.To(int32(1)),
			BackoffLimit:          ptr.To(int32(3)),
			ActiveDeadlineSeconds: ptr.To(int64(opts.Timeout.Seconds())),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: podSpec(opts.NodeName, container, initContainers, volumes),
			},
		},
	}

	if err := c.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("create SOS report Job for node %s: %w", opts.NodeName, err)
	}

	return job, nil
}

// CreateKubeconfigSecret creates a Secret containing a kubeconfig for the SOS report pod.
func CreateKubeconfigSecret(ctx context.Context, c client.Client, namespace, clusterName, caseID string, data []byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName(clusterName),
			Namespace:   namespace,
			Labels:      commonLabels(caseID),
			Annotations: commonAnnotations(clusterName),
		},
		Data: map[string][]byte{
			secretDataKey: data,
		},
	}
	if err := c.Create(ctx, secret); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("create kubeconfig secret: %w", err)
		}
	}
	return nil
}

// ListNamespaces returns namespace names in the cluster (best-effort, for shell completion).
func ListNamespaces(ctx context.Context, c client.Client) []string {
	nsList := &corev1.NamespaceList{}
	if err := c.List(ctx, nsList); err != nil {
		return nil
	}
	names := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		names = append(names, ns.Name)
	}
	return names
}

// ListNodes returns the list of node names in the cluster.
// If labelSelector is non-empty, only nodes matching the selector are returned.
func ListNodes(ctx context.Context, c client.Client, labelSelector string) ([]string, error) {
	nodes := &corev1.NodeList{}
	listOpts := []client.ListOption{}
	if labelSelector != "" {
		selector, err := labels.Parse(labelSelector)
		if err != nil {
			return nil, fmt.Errorf("parse node selector %q: %w", labelSelector, err)
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selector})
	}
	if err := c.List(ctx, nodes, listOpts...); err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	names := make([]string, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		names = append(names, node.Name)
	}
	return names, nil
}

// isNFSJob returns true if the Job was created with NFS output mode.
// NFS jobs use a "done" main container instead of the "sleep" container used for local downloads.
func isNFSJob(job *batchv1.Job) bool {
	containers := job.Spec.Template.Spec.Containers
	return len(containers) > 0 && containers[0].Name == "done"
}

// FindReadyDownloadPod finds a running pod for download (sleep strategy).
// With sleep strategy, the pod is Running and sleeping after sosreport completes.
func FindReadyDownloadPod(ctx context.Context, c client.Client, namespace string, job *batchv1.Job) (*corev1.Pod, error) {
	podSelector, err := metav1.LabelSelectorAsSelector(job.Spec.Selector)
	if err != nil {
		return nil, err
	}

	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: podSelector}); err != nil {
		return nil, err
	}
	for i := range podList.Items {
		if podList.Items[i].Status.Phase == corev1.PodRunning {
			return &podList.Items[i], nil
		}
	}
	return nil, nil
}

// ListJobs lists all sosreport Jobs in the namespace, optionally filtered by case ID.
func ListJobs(ctx context.Context, c client.Client, namespace, caseID string) ([]batchv1.Job, error) {
	labels := selectorLabels()
	if caseID != "" {
		labels[labelCaseID] = caseID
	}

	jobList := &batchv1.JobList{}
	if err := c.List(ctx, jobList, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		return nil, fmt.Errorf("list SOS report Jobs: %w", err)
	}
	return jobList.Items, nil
}

func shortNodeName(nodeName string) string {
	shortName, _, found := strings.Cut(nodeName, ".")
	if found && shortName != "" {
		return shortName
	}
	return nodeName
}

func shortHash(value string) string {
	return digest.Short(digest.FromObjects(value), hashLength)
}

func nodeLabelValue(nodeName string) string {
	name := dns1123NamePart(shortNodeName(nodeName))
	hash := shortHash(nodeName)
	maxNameLength := validation.LabelValueMaxLength - len(hash) - 1
	name = truncateDNS1123NamePart(name, maxNameLength)
	return fmt.Sprintf("%s-%s", name, hash)
}

func dns1123NamePart(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	name := b.String()
	return truncateDNS1123NamePart(name, len(name))
}

func truncateDNS1123NamePart(value string, maxLength int) string {
	if len(value) > maxLength {
		value = value[:maxLength]
	}
	return strings.Trim(value, "-")
}

// CreateKubeconfigDataFromConfig converts a rest.Config into kubeconfig YAML bytes.
func CreateKubeconfigDataFromConfig(config *rest.Config) ([]byte, error) {
	apiConfig := clientcmdapi.NewConfig()
	clusterName := "cluster"
	userName := "user"
	contextName := "context"

	apiConfig.Clusters[clusterName] = &clientcmdapi.Cluster{
		Server:                   config.Host,
		CertificateAuthorityData: config.CAData,
		InsecureSkipTLSVerify:    config.Insecure,
	}
	apiConfig.AuthInfos[userName] = &clientcmdapi.AuthInfo{
		ClientCertificateData: config.CertData,
		ClientKeyData:         config.KeyData,
		Token:                 config.BearerToken,
	}
	apiConfig.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  clusterName,
		AuthInfo: userName,
	}
	apiConfig.CurrentContext = contextName

	return clientcmd.Write(*apiConfig)
}

func podSpec(nodeName string, container corev1.Container, initContainers []corev1.Container, volumes []corev1.Volume) corev1.PodSpec {
	return corev1.PodSpec{
		NodeName:       nodeName,
		RestartPolicy:  corev1.RestartPolicyNever,
		HostIPC:        true,
		HostNetwork:    true,
		HostPID:        true,
		InitContainers: initContainers,
		Containers:     []corev1.Container{container},
		Volumes:        volumes,
	}
}

func baseVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{MountPath: "/host", Name: "host"},
		{MountPath: "/run", Name: "run"},
		{MountPath: "/var/log", Name: "varlog"},
		{MountPath: "/etc/kubernetes/admin.conf", Name: "adminconf", SubPath: secretDataKey},
		{MountPath: "/etc/localtime", Name: "localtime"},
		{MountPath: "/etc/machine-id", Name: "machineid"},
		{MountPath: "/boot", Name: "boot"},
		{MountPath: "/usr/lib/modules/", Name: "modules"},
	}
}

func baseVolumes(secretName string) []corev1.Volume {
	return []corev1.Volume{
		{Name: "host", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}},
		{Name: "run", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/run"}}},
		{Name: "boot", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/boot"}}},
		{Name: "modules", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/usr/lib/modules/"}}},
		{Name: "varlog", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/log"}}},
		{Name: "adminconf", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName}}},
		{Name: "localtime", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/etc/localtime"}}},
		{Name: "machineid", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/etc/machine-id"}}},
	}
}
