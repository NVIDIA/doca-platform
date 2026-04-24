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

package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/utils/tunnel"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

type Collector struct {
	clusters []*Cluster
}

func New(clusters []*Cluster) *Collector {
	return &Collector{
		clusters: clusters,
	}
}

type Cluster struct {
	clusterName  string
	client       client.Client
	artifactsDir string
	clientset    *kubernetes.Clientset
	tunnel       *tunnel.Tunnel
}

type ClusterCollector struct {
	Client     client.Client
	ClientSet  *kubernetes.Clientset
	RestConfig *rest.Config
}

func GetClusterCollectors(ctx context.Context, cc ClusterCollector, artifactsDirectory string) ([]*Cluster, error) {
	log := ctrllog.FromContext(ctx)
	directory := filepath.Join(artifactsDirectory, "main")
	mainCluster, err := NewMainCluster(cc.Client, directory, cc.ClientSet)
	if err != nil {
		// If the main cluster client isn't created return early.
		return nil, err
	}
	collectors := make([]*Cluster, 0)
	collectors = append(collectors, mainCluster)
	errs := make([]error, 0)
	// Get collectors for DPFClusters.
	clusterConfigs, err := dpucluster.GetConfigs(ctx, cc.Client)
	if err != nil {
		return nil, err
	}
	for _, conf := range clusterConfigs {
		restCfg, tun, err := tunnel.NewTunneledRestConfig(ctx, cc.Client, cc.RestConfig, conf.Cluster)
		if err != nil {
			errs = append(errs, fmt.Errorf("create tunnel for DPUCluster %s: %w", conf.Cluster.Name, err))
			continue
		}

		dpuClusterClient, err := client.New(restCfg, client.Options{})
		if err != nil {
			tun.Close()
			errs = append(errs, fmt.Errorf("create client for DPUCluster %s: %w", conf.Cluster.Name, err))
			continue
		}

		dpuClusterClientset, err := kubernetes.NewForConfig(restCfg)
		if err != nil {
			tun.Close()
			errs = append(errs, fmt.Errorf("create clientset for DPUCluster %s: %w", conf.Cluster.Name, err))
			continue
		}

		directory = filepath.Join(artifactsDirectory, conf.Cluster.Name)
		c, err := NewDPUCluster(dpuClusterClient, directory, dpuClusterClientset, conf.Cluster.Name, tun)
		if err != nil {
			tun.Close()
			errs = append(errs, err)
			continue
		}
		collectors = append(collectors, c)
	}
	if len(errs) > 0 {
		log.Error(kerrors.NewAggregate(errs), "Failed to create collectors for hosted control planes")
	}
	return collectors, nil
}

func NewMainCluster(client client.Client, artifactsDirectory string, clientset *kubernetes.Clientset) (*Cluster, error) {
	return &Cluster{
		clusterName:  "main",
		client:       client,
		artifactsDir: artifactsDirectory,
		clientset:    clientset,
	}, nil
}

func NewDPUCluster(client client.Client, artifactsDirectory string, clientset *kubernetes.Clientset, name string, tun *tunnel.Tunnel) (*Cluster, error) {
	return &Cluster{
		clusterName:  name,
		client:       client,
		artifactsDir: artifactsDirectory,
		clientset:    clientset,
		tunnel:       tun,
	}, nil
}

func (c *Collector) Close() {
	for _, cluster := range c.clusters {
		if cluster.tunnel != nil {
			cluster.tunnel.Close()
		}
	}
}

func (c *Cluster) Name() string {
	return c.clusterName
}

func (c *Collector) Run(ctx context.Context) error {
	log := ctrllog.FromContext(ctx)
	errs := make([]error, 0)
	for _, cluster := range c.clusters {
		log.Info(fmt.Sprintf("Running collector for %s", cluster.Name()))
		if err := cluster.run(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return kerrors.NewAggregate(errs)
}

func (c *Cluster) run(ctx context.Context) error {
	namespacesToCollectEvents := []string{
		"dpf-operator-system",
	}
	errs := make([]error, 0)
	resourcesToCollect, err := getResourcesToCollect(c.clientset)
	if err != nil {
		return err
	}

	for _, resource := range resourcesToCollect {
		err = c.dumpResource(ctx, resource)
		if err != nil {
			ctrllog.FromContext(ctx).Info(fmt.Sprintf("Cannot dump resource %s: %v", resource.String(), err))
		}
	}

	// Dump the events from all the pods on the cluster.
	err = c.dumpPodEvents(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("error dumping pod logs %w", err))
	}

	for _, ns := range namespacesToCollectEvents {
		if err := c.dumpEventsForNamespace(ctx, ns); err != nil {
			errs = append(errs, fmt.Errorf("error dumping events for namespace %s: %w", ns, err))
		}
	}
	return kerrors.NewAggregate(errs)
}

func getResourcesToCollect(clientset *kubernetes.Clientset) ([]schema.GroupVersionKind, error) {
	resourcesToCollect := []schema.GroupVersionKind{}
	resourceList, err := clientset.DiscoveryClient.ServerPreferredResources()
	if err != nil {
		return nil, fmt.Errorf("get supported resources with the preferred version: %w", err)
	}
	for _, rl := range resourceList {
		gv, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			continue
		}

		for _, r := range rl.APIResources {
			gvk := gv.WithKind(r.Kind)
			resourcesToCollect = append(resourcesToCollect, gvk)
		}
	}
	return resourcesToCollect, nil
}

func (c *Cluster) dumpPodEvents(ctx context.Context) error {
	podList := &corev1.PodList{}
	err := c.client.List(ctx, podList)
	if err != nil {
		return err
	}
	errs := []error{}
	for _, pod := range podList.Items {
		if err = c.dumpEventsForNamespacedResource(ctx, "Pod", types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}); err != nil {
			errs = append(errs, err)
		}
	}
	return kerrors.NewAggregate(errs)
}

func (c *Cluster) writeToFile(data []byte, filePath string) error {
	err := os.MkdirAll(filepath.Dir(filePath), 0750)
	if err != nil {
		return err
	}
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	err = os.WriteFile(f.Name(), data, 0600)
	if err != nil {
		return err
	}
	return nil
}

func (c *Cluster) dumpEventsForNamespacedResource(ctx context.Context, kind string, ref types.NamespacedName) error {
	fieldSelector := fmt.Sprintf("regarding.name=%s", ref.Name)
	events, _ := c.clientset.EventsV1().Events(ref.Namespace).List(ctx, metav1.ListOptions{FieldSelector: fieldSelector, TypeMeta: metav1.TypeMeta{Kind: kind}})
	filePath := filepath.Join(c.artifactsDir, "Events", kind, ref.Namespace, fmt.Sprintf("%v.events", ref.Name))
	if err := c.writeResourceToFile(events, filePath); err != nil {
		return err
	}
	return nil
}

func (c *Cluster) dumpEventsForNamespace(ctx context.Context, namespace string) error {
	events, err := c.clientset.EventsV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error while listing events: %w", err)
	}
	filePath := filepath.Join(c.artifactsDir, "Events", "Namespace", fmt.Sprintf("%v.events", namespace))
	if err := c.writeResourceToFile(events, filePath); err != nil {
		return err
	}
	return nil
}

func (c *Cluster) dumpResource(ctx context.Context, kind schema.GroupVersionKind) error {
	resourceList := unstructured.UnstructuredList{}
	resourceList.SetKind(kind.Kind)
	resourceList.SetAPIVersion(kind.GroupVersion().String())
	if err := c.client.List(ctx, &resourceList); err != nil {
		return err
	}
	for _, resource := range resourceList.Items {
		filePath := filepath.Join(c.artifactsDir, "Resources", resource.GetObjectKind().GroupVersionKind().Kind, resource.GetNamespace(), fmt.Sprintf("%v.yaml", resource.GetName()))
		err := c.writeResourceToFile(&resource, filePath)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Cluster) writeResourceToFile(resource runtime.Object, filePath string) error {
	yaml, err := yaml.Marshal(resource)
	if err != nil {
		return err
	}
	if err := c.writeToFile(yaml, filePath); err != nil {
		return err
	}
	return nil
}
