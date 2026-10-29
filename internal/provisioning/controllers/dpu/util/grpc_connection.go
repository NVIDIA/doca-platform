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

package util

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateGRPCConnection(ctx context.Context, c client.Client, dpu *provisioningv1.DPU, ctrlContext *ControllerContext) (*grpc.ClientConn, error) {
	dpuNode := &provisioningv1.DPUNode{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Spec.DPUNodeName}, dpuNode); err != nil {
		return nil, err
	} else if dpuNode.Spec.NodeDMSAddress.String() == "" {
		return nil, fmt.Errorf("DMS address is not set")
	}

	dmsClientSecretName := dms.DMSClientSecret
	if ctrlContext.Options.CustomCASecretName != "" {
		dmsClientSecretName = ctrlContext.Options.CustomCASecretName
	}

	nn := types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      dmsClientSecretName,
	}
	dmsClientSecret := &corev1.Secret{}
	if err := c.Get(ctx, nn, dmsClientSecret); err != nil {
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

	// Extract the CA certificate
	caCert, caCertOk := dmsClientSecret.Data["ca.crt"]
	if !caCertOk {
		return nil, fmt.Errorf("ca.crt not found in Server secret")
	}

	// Create a certificate pool and add the CA certificate
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to append Server certificate")
	}

	// Create a mTLS config with the client certificate and CA certificate
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
		NextProtos:   []string{"http/1.1"},
	}

	// Create a gRPC connection using grpc.NewClient
	conn, err := grpc.NewClient(dpuNode.Spec.NodeDMSAddress.String(), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %v", err)
	}

	return conn, nil
}
