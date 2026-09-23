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

package node

import "testing"

func TestParseJoinCommand(t *testing.T) {
	cmd := "kubeadm join 10.0.110.10:6443 --token abcdef.0123456789abcdef --v=5 " +
		"--discovery-token-ca-cert-hash sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef " +
		"--discovery-token-ca-cert-hash sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	jc, err := ParseJoinCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if jc.Server != "10.0.110.10:6443" || jc.Token != "abcdef.0123456789abcdef" || len(jc.CACertHashes) != 2 {
		t.Fatalf("unexpected join command %+v", *jc)
	}
	for _, bad := range []string{
		"",
		"kubeadm init",
		"kubeadm join 1.2.3.4:6443 --v=5",
		"kubeadm join 1.2.3.4:6443 --token abcdef.0123456789abcdef",
	} {
		if _, err := ParseJoinCommand(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
