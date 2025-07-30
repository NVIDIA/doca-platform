/*
Copyright 2025 NVIDIA

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

package redfish

import (
	"context"
	b64 "encoding/base64"
	"fmt"
	"net/http"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"

	"github.com/mcuadros/go-version"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	BMCMinSupportedVersion = "BF-24.10"
)

func InitializeInterface(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	logger := log.FromContext(ctx)

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	basicAuthClient, err := rfclient.InitPassword(ctx, dpu.Spec.BMCIP, dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		err = fmt.Errorf("failed to initialize password: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToInitializePassword", err.Error()))
		return *state, err
	}

	_, data, err := basicAuthClient.CheckBMCFirmware()
	if err != nil {
		err = fmt.Errorf("failed to get BMC firmware: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckBMCFW", err.Error()))
		return *state, err
	}

	if version.Compare(data.Version, BMCMinSupportedVersion, "<") {
		taskName := fmt.Sprintf("%s-%s", dpu.Name, dpu.UID)

		if taskID, ok := dutil.BmcFwUpdateTaskMap.Load(taskName); ok {
			switch taskID := taskID.(type) {
			case string:
				// check progress
				resp, prog, err := basicAuthClient.CheckTaskProgress(taskID)
				if err != nil {
					err = fmt.Errorf("failed to check task progress: %w", err)
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailToCheckProgress", err.Error()))
					return *state, err
				} else if resp.StatusCode() != http.StatusOK {
					err = fmt.Errorf("get status: %s is not OK", resp.Status())
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailToCheckProgress", err.Error()))
					return *state, err
				}
				logger.Info(fmt.Sprintf("task: %+v", prog))
				switch prog.TaskState {
				case "Exception":
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailToInstall", fmt.Sprintf("Task %s is in Exception state: %v", taskID, prog.Messages)))
					state.Phase = provisioningv1.DPUError
					return *state, nil
				case "New", "Starting", "Running":
					logger.Info(fmt.Sprintf("taskProgress: %+v", prog.PercentComplete))
					return *state, nil
				case "Completed":
					logger.Info("Task completed. Resetting BMC")
					_, _, err := basicAuthClient.ResetBMC()
					if err != nil {
						err = fmt.Errorf("failed to reset BMC: %w", err)
						cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailToResetBMC", err.Error()))
						return *state, err
					}
					dutil.BmcFwUpdateTaskMap.Delete(taskName)
					return *state, nil
				default:
					err = fmt.Errorf("unknown task state: '%s'", prog.TaskState)
					logger.Info(err.Error())
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "UnknownSTate", err.Error()))
					return *state, err
				}
			case dutil.TaskWithRetry:
				updateTask := taskID.Task
				if updateTask.GetState() == future.Ready {
					if _, err := updateTask.GetResult(); err != nil {
						err = fmt.Errorf("failed to update BMC firmware: %w", err)
						cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailToUpdateBMCFW", err.Error()))
						state.Phase = provisioningv1.DPUError
						dutil.BmcFwUpdateTaskMap.Delete(taskName)
						return *state, nil
					}
				}
				return *state, nil
			}

		} else {
			logger.Info(fmt.Sprintf("Current BMC FW: %s is older than 24.10, update to 24.10-17", data.Version))
			updateTask := future.New(func() (any, error) {
				fwFile, err := os.Open("/bf3-bmc.fwpkg")
				if err != nil {
					err = fmt.Errorf("failed to open BMC firmware file: %w", err)
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToOpenBMCFirmware", err.Error()))
					return *state, err
				}
				defer func() {
					_ = fwFile.Close()
				}()
				resp, taskInfo, err := basicAuthClient.UpdateBMCFirmware(fwFile)
				if err != nil {
					err = fmt.Errorf("failed to update BMC firmware: %w", err)
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailToUpdateBMCFW", err.Error()))
					state.Phase = provisioningv1.DPUError
					return *state, err
				} else if resp.StatusCode() != http.StatusAccepted {
					err = fmt.Errorf("get status: %s", resp.Status())
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailToUpdateBMCFW", err.Error()))
					state.Phase = provisioningv1.DPUError
					return *state, err
				}
				dutil.BmcFwUpdateTaskMap.Swap(taskName, taskInfo.ID)
				logger.Info(fmt.Sprintf("new install task: %+v", *taskInfo))
				return taskInfo.ID, nil
			})
			taskWithRetryCount := dutil.TaskWithRetry{
				Task:       updateTask,
				RetryCount: 0,
			}
			dutil.BmcFwUpdateTaskMap.Store(taskName, taskWithRetryCount)

			return *state, nil
		}
	}

	_, err = rfclient.NewTLSClient(ctx, dpu, ctrlCtx.Client)
	if err != nil {
		log.FromContext(ctx).V(3).Info("failed to create tls client", err)

		if err = setUpMTLS(ctx, dpu, ctrlCtx, basicAuthClient); err != nil {
			err = fmt.Errorf("failed to set up mTLS: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToSetUpMTLS", err.Error()))
			return *state, err
		}
	}

	result, err := checkCapacity(ctx, dpu, ctrlCtx)
	if err != nil {
		err = fmt.Errorf("failed to check capacity: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckCapacity", err.Error()))
		return *state, err
	} else if result == dutil.CapacityUnknown {
		// send a warning in the condition message, but continue the flow
		state.Phase = provisioningv1.DPUConfigFWParameters
		cond := cutil.NewCondition(
			string(provisioningv1.DPUCondInterfaceInitialized), nil, "UnableToCheckResources",
			fmt.Sprintf("WARNING: unable to check DPU CPU/Memory capacity, the DPUFlavor may be unfit for the DPU, err: %v", err))
		cutil.SetDPUCondition(state, cond)
		return *state, err
	} else if result == dutil.CapacityInsufficient {
		err = fmt.Errorf("not enough resources for the given DPUFlavor")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckResources", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}
	state.Phase = provisioningv1.DPUConfigFWParameters
	cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), nil, "", ""))
	return *state, nil
}

// setUpMTLS sets up BMC mTLS in the same way as https://github.com/openbmc/bmcweb/blob/master/scripts/generate_auth_certificates.py
func setUpMTLS(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, basicAuthClient *rfclient.Client) error {
	caSecret := &corev1.Secret{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Name: rfclient.CASecret, Namespace: dpu.Namespace}, caSecret); err != nil {
		return fmt.Errorf("failed to get CA cert, err: %v", err)
	}
	caCert, ok := caSecret.Data["tls.crt"]
	if !ok {
		return fmt.Errorf("no CA cert in secret %s", caSecret.Name)
	}

	// step 1: install or replace CA certificate
	resp, _, err := basicAuthClient.InstallCert(string(caCert))
	if err != nil {
		return fmt.Errorf("failed to install CA cert, err: %v", err)
	} else if resp.StatusCode() == http.StatusInternalServerError {
		log.FromContext(ctx).Info("An existing CA certificate is likely already installed. Replacing...")
		resp, _, err = basicAuthClient.ReplaceCACert(string(caCert))
		if err != nil {
			return fmt.Errorf("failed to replace CA cert, err: %v", err)
		} else if resp.StatusCode() != http.StatusOK {
			return fmt.Errorf("failed to replace CA cert, unexpected response status: %s", resp.Status())
		}
		log.FromContext(ctx).Info("Successfully replaced CA certificate")
	} else if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to install CA cert, unexpected response status: %s", resp.Status())
	}

	// step 2: replace server certificate
	log.FromContext(ctx).Info("Replace server certificate...")
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	})
	err = ctrlCtx.Client.Get(ctx, types.NamespacedName{Name: dpu.Name, Namespace: dpu.Namespace}, cr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("cert-manager CertificateRequest does not exist, try create one...")
			resp, csrInfo, err := basicAuthClient.GenerateCSR(dpu.Spec.BMCIP)
			if err != nil {
				return fmt.Errorf("failed to generate CSR, err: %v", err)
			} else if resp.StatusCode() != http.StatusOK {
				return fmt.Errorf("failed to generate CSR, unexpected response status: %s", resp.Status())
			}
			if err := createCR(ctx, dpu, ctrlCtx, csrInfo.CSRString); err != nil {
				return fmt.Errorf("failed to create cert-manager CertificateRequest, err: %v", err)
			}
			log.FromContext(ctx).Info("successfully created cert-manager CertificateRequest")
		} else {
			return fmt.Errorf("failed to get existing cert-manager CertificateRequest, err: %v", err)

		}
	}

	certificate, found, err := unstructured.NestedString(cr.Object, "status", "certificate")
	if err != nil {
		return fmt.Errorf("failed to extract certificate %w", err)
	}
	if !found {
		return fmt.Errorf("cert-manager CertificateRequest is not issued yet, retry later")
	}

	decodedCert, err := b64.StdEncoding.DecodeString(certificate)
	if err != nil {
		return fmt.Errorf("failed to base64 decode certificate %w", err)
	}

	resp, _, err = basicAuthClient.ReplaceServerCert(string(decodedCert))
	if err != nil {
		return fmt.Errorf("failed to replace server cert, err: %v", err)
	} else if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to replace server cert, unexpected response status: %s", resp.Status())
	}
	log.FromContext(ctx).Info("Successfully replaced server certificate")

	// step 3: install client certificate
	log.FromContext(ctx).Info("Install client certificate...")
	clientSecret := &corev1.Secret{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Name: rfclient.ClientCertSecret, Namespace: dpu.Namespace}, clientSecret); err != nil {
		return fmt.Errorf("failed to get client cert, err: %v", err)
	}
	clientCert, ok := clientSecret.Data["tls.crt"]
	if !ok {
		return fmt.Errorf("no client cert in client secret %s", clientSecret.Name)
	}
	resp, _, err = basicAuthClient.InstallCert(string(clientCert))
	if err != nil {
		return fmt.Errorf("failed to install client cert, err: %v", err)
	} else if resp.StatusCode() == http.StatusInternalServerError {
		log.FromContext(ctx).Info("An existing client certificate is likely already installed. Skip installing client certificate")
	} else if resp.StatusCode() == http.StatusOK {
		log.FromContext(ctx).Info("Successfully installed client certificate")
	} else {
		return fmt.Errorf("failed to install client cert, unexpected response status: %s", resp.Status())
	}

	// step 4: enable mTLS
	log.FromContext(ctx).Info("enable mTLS...")
	resp, _, err = basicAuthClient.EnableMTLS()
	if err != nil {
		return fmt.Errorf("failed to enable mTLS, err: %v", err)
	} else if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to enable mTLS, unexpected response status: %s", resp.Status())
	}
	log.FromContext(ctx).Info("Successfully enabled mTLS")
	return nil
}

func generateCR(dpu *provisioningv1.DPU, csr string) (*unstructured.Unstructured, error) {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	})
	cr.SetName(dpu.Name)
	cr.SetNamespace(dpu.Namespace)
	cr.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(dpu, provisioningv1.DPUGroupVersionKind)})
	err := unstructured.SetNestedMap(cr.Object, map[string]interface{}{
		"request": b64.StdEncoding.EncodeToString([]byte(csr)),
		"isCA":    false,
		"usages": []interface{}{
			"server auth",
			"key encipherment",
			"digital signature",
		},
		"duration": metav1.Duration{
			// 365 days
			Duration: 8796 * time.Hour,
		}.ToUnstructured(),
		"issuerRef": map[string]interface{}{
			"name":  rfclient.Issuer,
			"kind":  "Issuer",
			"group": "cert-manager.io",
		},
	}, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to generate spec to CertificateRequest: %w", err)
	}
	return cr, nil
}

// createCR creates a cert-manager CertificateRequest for the given CSR
func createCR(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, csr string) error {
	cr, err := generateCR(dpu, csr)
	if err != nil {
		return err
	}
	return ctrlCtx.Client.Create(ctx, cr)
}

// checkCapacity checks if the DPU has sufficient resources for the flavor.
func checkCapacity(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (dutil.CapacityResult, error) {
	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Name: dpu.Spec.DPUFlavor, Namespace: dpu.Namespace}, flavor); err != nil {
		return dutil.CapacityUnknown, err
	}
	tlsClient, err := rfclient.NewTLSClient(ctx, dpu, ctrlCtx.Client)
	if err != nil {
		return dutil.CapacityUnknown, err
	}
	check := func(data string, parseFunc func(string) *dutil.BlueFieldSpecs) dutil.CapacityResult {
		bfSpecs := parseFunc(data)
		if bfSpecs == nil {
			return dutil.CapacityUnknown
		}
		log.FromContext(ctx).Info("retrieved DPU specs", "bfSpecs", bfSpecs)
		return bfSpecs.CanSatisfy(flavor.Spec.DPUResources)
	}

	// check capacity by part number
	resp, pn, err := tlsClient.GetChassis()
	if err != nil || resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("failed to get part number, status code: %s, err: %v", resp.Status(), err)
		return dutil.CapacityUnknown, err
	}
	if result := check(pn.PartNumber, dutil.LookUpPartNumber); result != dutil.CapacityUnknown {
		return result, nil
	}

	// check capacity by description
	resp, desc, err := tlsClient.GetProductDescription()
	if err != nil || resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("failed to get description, status code: %s, err: %v", resp.Status(), err)
		return dutil.CapacityUnknown, err
	}
	return check(desc.Description, dutil.ParseDescription), nil
}
