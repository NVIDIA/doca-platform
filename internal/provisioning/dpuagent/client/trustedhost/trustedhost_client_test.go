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

package trustedhost

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/service/types"

	"github.com/emicklei/go-restful/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	serverEndpoint = "http://localhost:11029"
)

var _ = Describe("TrustedhostClient", func() {
	var dpu *provisioningv1.DPU
	var server *http.Server

	Describe("runWithRetry", func() {
		It("should retry on error and eventually succeed", func() {
			callCount := 0
			failUntil := 2 // fail first 2 attempts, succeed on 3rd
			resp, err := runWithRetry(func() (*http.Response, error) {
				callCount++
				if callCount <= failUntil {
					return nil, net.ErrClosed
				}
				return &http.Response{StatusCode: http.StatusOK}, nil
			}, 10, 1*time.Millisecond)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(callCount).To(Equal(3))
		})

		It("should return error after all retries exhausted", func() {
			callCount := 0
			expectedErr := fmt.Errorf("test error")
			resp, err := runWithRetry(func() (*http.Response, error) {
				callCount++
				return nil, expectedErr
			}, 5, 1*time.Millisecond)
			Expect(err).To(Equal(expectedErr))
			Expect(resp).To(BeNil())
			Expect(callCount).To(Equal(5))
		})
	})

	var getObjectHandler = func(objs map[metav1.GroupVersionKind][]client.Object) restful.RouteFunction {
		return func(req *restful.Request, resp *restful.Response) {
			group := req.QueryParameter("group")
			version := req.QueryParameter("version")
			kind := req.QueryParameter("kind")
			namespace := req.QueryParameter("namespace")
			name := req.QueryParameter("name")
			gvk := metav1.GroupVersionKind{
				Group:   group,
				Version: version,
				Kind:    kind,
			}
			if objs == nil {
				resp.WriteHeader(http.StatusNotFound)
				return
			}
			objs, ok := objs[gvk]
			if !ok {
				resp.WriteHeader(http.StatusNotFound)
				return
			}
			for _, obj := range objs {
				if obj.GetName() == name && obj.GetNamespace() == namespace {
					_ = resp.WriteEntity(obj)
					return
				}
			}
			resp.WriteHeader(http.StatusNotFound)
		}
	}

	var runMockServer = func(updateStatusHandler restful.RouteFunction, healthCheckHandler restful.RouteFunction, getObjectHandler restful.RouteFunction) {
		parsedURL, err := url.Parse(serverEndpoint)
		Expect(err).To(Succeed())

		ws := new(restful.WebService).Path("/")
		ws.Route(
			ws.POST("/update-status").
				Consumes(restful.MIME_JSON).
				Produces(restful.MIME_JSON).
				To(updateStatusHandler))
		ws.Route(ws.GET("/healthz").To(healthCheckHandler))
		ws.Route(
			ws.GET("/get-object").
				Param(ws.QueryParameter("group", "the API group of the object (empty for core API group)")).
				Param(ws.QueryParameter("version", "the API version of the object").Required(true)).
				Param(ws.QueryParameter("kind", "the kind of the object").Required(true)).
				Param(ws.QueryParameter("namespace", "the namespace of the object (empty for cluster-scoped objects)")).
				Param(ws.QueryParameter("name", "the name of the object").Required(true)).
				Produces(restful.MIME_JSON).To(getObjectHandler))
		container := restful.NewContainer()
		container.Add(ws)
		server = &http.Server{
			Addr:    parsedURL.Host,
			Handler: container,
		}
		go server.ListenAndServe() //nolint:errcheck
		Eventually(func(g Gomega) {
			_, err := net.DialTimeout("tcp", parsedURL.Host, 5*time.Second)
			g.Expect(err).NotTo(HaveOccurred())
		}).WithTimeout(1 * time.Minute).Should(Succeed())
	}

	BeforeEach(func() {
		dpu = &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dpu", Namespace: "test-namespace"},
		}
	})

	AfterEach(func() {
		if server != nil {
			Expect(server.Close()).To(Succeed())
		}
	})

	Describe("HealthCheck", func() {
		It("health check should succeed", func() {
			runMockServer(func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusOK)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusOK)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusInternalServerError)
			})
			client := NewTrustedhostClient(dpu.Name, dpu.Namespace)
			client.hostAgentEndpoint = serverEndpoint
			err := client.HealthCheck()
			Expect(err).NotTo(HaveOccurred())
		})

		It("health check should fail", func() {
			runMockServer(func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusInternalServerError)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusInternalServerError)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusInternalServerError)
			})
			client := NewTrustedhostClient(dpu.Name, dpu.Namespace)
			client.hostAgentEndpoint = serverEndpoint
			err := client.HealthCheck()
			Expect(err).To(HaveOccurred())
		})

		It("update status should succeed", func() {
			receivedRequest := types.UpdateStatusRequest{}
			runMockServer(func(req *restful.Request, resp *restful.Response) {
				err := req.ReadEntity(&receivedRequest)
				Expect(err).NotTo(HaveOccurred())
				Expect(receivedRequest.DPUName).To(Equal(dpu.Name))
				Expect(receivedRequest.DPUNamespace).To(Equal(dpu.Namespace))
				resp.WriteHeader(http.StatusOK)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusOK)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusInternalServerError)
			})
			client := NewTrustedhostClient(dpu.Name, dpu.Namespace)
			client.hostAgentEndpoint = serverEndpoint
			agentStatus := provisioningv1.AgentStatus{
				HostRebootRequired: ptr.To(true),
				Conditions: []metav1.Condition{
					{
						Type:    "Ready",
						Status:  metav1.ConditionTrue,
						Reason:  "TestReason",
						Message: "TestMessage",
					},
				},
			}
			err := client.UpdateStatus(ctx, agentStatus)
			Expect(err).NotTo(HaveOccurred())
			Expect(equality.Semantic.DeepEqual(receivedRequest.AgentStatus, agentStatus)).To(BeTrue())
		})

		It("update status should fail", func() {
			runMockServer(func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusInternalServerError)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusInternalServerError)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusInternalServerError)
			})
			client := NewTrustedhostClient(dpu.Name, dpu.Namespace)
			client.hostAgentEndpoint = serverEndpoint
			err := client.UpdateStatus(ctx, provisioningv1.AgentStatus{
				HostRebootRequired: ptr.To(true),
				Conditions: []metav1.Condition{
					{
						Type:    "Ready",
						Status:  metav1.ConditionTrue,
						Reason:  "TestReason",
						Message: "TestMessage",
					},
				},
			})
			Expect(err).To(HaveOccurred())
		})

		It("should be able to get cluster scoped object", func() {
			mockObjects := make(map[metav1.GroupVersionKind][]client.Object)
			mockObjects[metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}] = []client.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}}
			runMockServer(func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusOK)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusOK)
			}, getObjectHandler(mockObjects))
			client := NewTrustedhostClient(dpu.Name, dpu.Namespace)
			client.hostAgentEndpoint = serverEndpoint
			respObject := &corev1.Namespace{}
			err := client.GetObject(ctx, "", "test", respObject)
			Expect(err).NotTo(HaveOccurred())
			Expect(respObject.Name).To(Equal("test"))
		})

		It("should be able to get namespaced object", func() {
			mockObjects := make(map[metav1.GroupVersionKind][]client.Object)
			mockObjects[metav1.GroupVersionKind{Group: provisioningv1.GroupVersion.Group, Version: provisioningv1.GroupVersion.Version, Kind: provisioningv1.DPUKind}] = []client.Object{dpu}
			runMockServer(func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusOK)
			}, func(req *restful.Request, resp *restful.Response) {
				resp.WriteHeader(http.StatusOK)
			}, getObjectHandler(mockObjects))
			client := NewTrustedhostClient(dpu.Name, dpu.Namespace)
			client.hostAgentEndpoint = serverEndpoint
			err := client.GetObject(ctx, dpu.Namespace, dpu.Name, dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(dpu.Name).To(Equal(dpu.Name))
			Expect(dpu.Namespace).To(Equal(dpu.Namespace))
			Expect(dpu.Spec).To(Equal(dpu.Spec))
			Expect(dpu.Status).To(Equal(dpu.Status))
		})
	})

})
