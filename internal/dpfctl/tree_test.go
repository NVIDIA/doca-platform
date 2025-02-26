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

package dpfctl

import (
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func Test_hasSameReadyStatusAndReason(t *testing.T) {
	readyTrue := &metav1.Condition{Type: string(conditions.TypeReady), Status: metav1.ConditionTrue}
	readyFalseReasonInfo := &metav1.Condition{Type: string(conditions.TypeReady), Status: metav1.ConditionFalse, Reason: "Info"}

	type args struct {
		a *metav1.Condition
		b *metav1.Condition
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Objects without conditions are the same",
			args: args{
				a: nil,
				b: nil,
			},
			want: true,
		},
		{
			name: "Objects with same Ready condition are the same",
			args: args{
				a: readyTrue,
				b: readyTrue,
			},
			want: true,
		},
		{
			name: "Objects with different Ready.Status are not the same",
			args: args{
				a: readyTrue,
				b: readyFalseReasonInfo,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got := hasSameStatusAndReason(tt.args.a, tt.args.b)
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func Test_minLastTransitionTime(t *testing.T) {
	now := &metav1.Condition{Type: "now", LastTransitionTime: metav1.Now()}
	beforeNow := &metav1.Condition{Type: "beforeNow", LastTransitionTime: metav1.Time{Time: now.LastTransitionTime.Time.Add(-1 * time.Hour)}}
	type args struct {
		a *metav1.Condition
		b *metav1.Condition
	}
	tests := []struct {
		name string
		args args
		want metav1.Time
	}{
		{
			name: "nil, nil should return empty time",
			args: args{
				a: nil,
				b: nil,
			},
			want: metav1.Time{},
		},
		{
			name: "nil, now should return now",
			args: args{
				a: nil,
				b: now,
			},
			want: now.LastTransitionTime,
		},
		{
			name: "now, nil should return now",
			args: args{
				a: now,
				b: nil,
			},
			want: now.LastTransitionTime,
		},
		{
			name: "now, beforeNow should return beforeNow",
			args: args{
				a: now,
				b: beforeNow,
			},
			want: beforeNow.LastTransitionTime,
		},
		{
			name: "beforeNow, now should return beforeNow",
			args: args{
				a: now,
				b: beforeNow,
			},
			want: beforeNow.LastTransitionTime,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got := minLastTransitionTime(tt.args.a, tt.args.b)
			g.Expect(got.Time).To(BeTemporally("~", tt.want.Time))
		})
	}
}

func Test_isObjDebug(t *testing.T) {
	obj := fakeDPUService("flannel")
	type args struct {
		filter string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "empty filter should return false",
			args: args{
				filter: "",
			},
			want: false,
		},
		{
			name: "all filter should return true",
			args: args{
				filter: "all",
			},
			want: true,
		},
		{
			name: "kind filter should return true",
			args: args{
				filter: "DPUService",
			},
			want: true,
		},
		{
			name: "another kind filter should return false",
			args: args{
				filter: "AnotherKind",
			},
			want: false,
		},
		{
			name: "kind/name filter should return true",
			args: args{
				filter: "DPUService/flannel",
			},
			want: true,
		},
		{
			name: "kind/wrong name filter should return false",
			args: args{
				filter: "DPU/worker1-0000-08-00",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got := isObjDebug(obj, tt.args.filter)
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func fakeDPUService(name string) *dpuservicev1.DPUService {
	m := &dpuservicev1.DPUService{
		TypeMeta: metav1.TypeMeta{
			Kind: "DPUService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      name,
			UID:       types.UID(name),
		},
	}
	return m
}
