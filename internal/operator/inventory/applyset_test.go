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

package inventory

import (
	"reflect"
	"testing"
)

func TestParseGroupKindNamespaceName(t *testing.T) {

	tests := []struct {
		name    string
		input   string
		want    GroupKindNamespaceName
		wantErr bool
	}{
		{
			name:  "correctly parse GKNN for pod with no Group",
			input: "Pod./pod-1.default",
			want: GroupKindNamespaceName{
				Group:     "",
				Kind:      "Pod",
				Namespace: "default",
				Name:      "pod-1",
			},
		},
		{
			name: "correctly parse GKNN from GKNN.String()",
			input: GroupKindNamespaceName{
				Group:     "",
				Kind:      "Pod",
				Namespace: "default",
				Name:      "pod-1",
			}.String(),
			want: GroupKindNamespaceName{
				Group:     "",
				Kind:      "Pod",
				Namespace: "default",
				Name:      "pod-1",
			},
		},
		{
			name:    "error if gknn has no / separator",
			wantErr: true,
			input:   "Group.Kindpod-1.default",
		},
		{
			name:    "error if groupkind parts is missing '.' separator",
			wantErr: true,
			input:   "MissingDotGroupKind/pod-1.default",
		},
		{
			name:    "error if namespace name is missing '.' separator",
			wantErr: true,
			input:   "MissingDotGroup.Kind/pod-1defaultagain",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGroupKindNamespaceName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGroupKindNamespaceName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseGroupKindNamespaceName() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplySetID(t *testing.T) {
	t.Run("generate stabled ID for ApplySet", func(t *testing.T) {
		component := &fromDPUService{name: "first"}
		want := "applyset-k9u1AQY4bVGhETCxPxlJdlzUqIc5LeeQeBEzjAXSwKg-v1"
		if got := ApplySetID("namespace-one", component); got != want {
			t.Errorf("ApplySetID() = %v, want %v", got, want)
		}
	})
}
