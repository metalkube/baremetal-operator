/*
Copyright 2019 The Kubernetes Authors.

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

package v1alpha1

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func errorContains(out error, want string) bool {
	if out == nil {
		return want == ""
	}
	if want == "" {
		return false
	}
	return strings.Contains(out.Error(), want)
}

func TestBareMetalHostCreate(t *testing.T) {
	tests := []struct {
		name      string
		bmh       *BareMetalHost
		wantedErr string
	}{
		{
			name: "valid",
			bmh: &BareMetalHost{TypeMeta: metav1.TypeMeta{
				Kind:       "BareMetalHost",
				APIVersion: "metal3.io/v1alpha1",
			}, ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test-namespace",
			}, Spec: BareMetalHostSpec{}},
			wantedErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.bmh.ValidateCreate(); !errorContains(err, tt.wantedErr) {
				t.Errorf("BareMetalHost.ValidateCreate() error = %v, wantErr %v", err, tt.wantedErr)
			}
		})
	}
}

func TestBareMetalHostUpdate(t *testing.T) {
	tests := []struct {
		name      string
		bmh       *BareMetalHost
		old       *BareMetalHost
		wantedErr string
	}{
		{
			name: "valid",
			bmh: &BareMetalHost{TypeMeta: metav1.TypeMeta{
				Kind:       "BareMetalHost",
				APIVersion: "metal3.io/v1alpha1",
			}, ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test-namespace",
			}, Spec: BareMetalHostSpec{}},
			old: &BareMetalHost{TypeMeta: metav1.TypeMeta{
				Kind:       "BareMetalHost",
				APIVersion: "metal3.io/v1alpha1",
			}, ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test-namespace",
			}, Spec: BareMetalHostSpec{}},
			wantedErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.bmh.ValidateUpdate(tt.old); !errorContains(err, tt.wantedErr) {
				t.Errorf("BareMetalHost.ValidateUpdate() error = %v, wantErr %v", err, tt.wantedErr)
			}
		})
	}
}
