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
	"net"
	"net/http"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	BMCUser              = "root"
	BMCPasswordSecret    = "bmc-shared-password"
	BMCSharedPasswordKey = "password"
	BMCDefaultPassword   = "0penBmc"
)

func InitializeInterface(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	_, err := rfclient.NewTLSClient(ctx, dpu, ctrlCtx.Client)
	if err != nil {
		log.FromContext(ctx).V(3).Info("failed to create tls client", err)
		basicAuthClient, err := initPassword(ctx, dpu, ctrlCtx)
		if err != nil {
			err = fmt.Errorf("failed to initialize password: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToInitializePassword", err.Error()))
			return *state, err
		}

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

// initPassword reads a password from the user-created Secret and set it to all DPUs
func initPassword(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (*rfclient.Client, error) {
	if net.ParseIP(dpu.Spec.BMCIP) == nil {
		return nil, fmt.Errorf("invalid IP: %s", dpu.Spec.BMCIP)
	}

	// get BMC password from the secret created by user
	nn := types.NamespacedName{Name: BMCPasswordSecret, Namespace: dpu.Namespace}
	passwdSecret := &corev1.Secret{}
	if err := ctrlCtx.Get(ctx, nn, passwdSecret); err != nil {
		return nil, err
	}
	passwd := string(passwdSecret.Data[BMCSharedPasswordKey])
	if passwd == "" {
		return nil, fmt.Errorf("password not specified in secret %s", nn.String())
	}

	// check if the default password has been changed as requested by the DOCA BMC manual
	client, err := rfclient.NewBasicAuthClient(dpu.Spec.BMCIP, BMCUser, passwd)
	if err != nil {
		return nil, err
	}
	resp, _, err := client.CheckBMCFirmware()
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized:
		log.FromContext(ctx).Info("try to change password")
		defaultClient, err := rfclient.NewBasicAuthClient(dpu.Spec.BMCIP, BMCUser, BMCDefaultPassword)
		if err != nil {
			return nil, err
		}
		resp, _, err = defaultClient.ChangeBMCPassword(passwd)
		if err != nil {
			return nil, err
		} else if resp.StatusCode() == http.StatusUnauthorized {
			return nil, fmt.Errorf("the default BMC password has been changed and the given password is wrong")
		} else if resp.StatusCode() != http.StatusOK {
			return nil, fmt.Errorf("expected status %q, received status %q", http.StatusOK, resp.Status())
		}
		log.FromContext(ctx).Info("successfully changed password")
		return client, nil
	case http.StatusOK:
		return client, nil
	default:
		return nil, fmt.Errorf("unexpected status: %q", resp.Status())
	}
}

// setUpMTLS sets up BMC mTLS in the same way as https://gitlab-master.nvidia.com/-/snippets/8219
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
	resp, pn, err := tlsClient.GetPartNumber()
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
