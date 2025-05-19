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

package common

import (
	"github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CheckGRPCErr ensures that err contains expected GRPC error
func CheckGRPCErr(err error, expectedCode codes.Code, message string) {
	gomega.ExpectWithOffset(1, err).To(gomega.HaveOccurred())
	status, ok := status.FromError(err)
	gomega.ExpectWithOffset(1, ok).To(gomega.BeTrue(), "err should contain GRPC status")
	gomega.ExpectWithOffset(1, status.Code()).To(gomega.Equal(expectedCode))
	gomega.ExpectWithOffset(1, status.Message()).To(gomega.ContainSubstring(message))
}
