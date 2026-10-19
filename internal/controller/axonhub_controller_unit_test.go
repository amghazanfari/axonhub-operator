/*
Copyright 2026.

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

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestPullPolicyFor(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  corev1.PullPolicy
	}{
		{name: "latest tag is always pulled", image: "looplj/axonhub:latest", want: corev1.PullAlways},
		{name: "untagged image means latest", image: "looplj/axonhub", want: corev1.PullAlways},
		{name: "pinned tag is pulled if not present", image: "looplj/axonhub:v1.2.3", want: corev1.PullIfNotPresent},
		{name: "registry with port and pinned tag", image: "localhost:5000/axonhub:v1.2.3", want: corev1.PullIfNotPresent},
		{name: "registry with port and latest tag", image: "localhost:5000/axonhub:latest", want: corev1.PullAlways},
		{name: "registry with port and no tag", image: "localhost:5000/axonhub", want: corev1.PullAlways},
		{name: "digest reference is immutable", image: "looplj/axonhub@sha256:abc123", want: corev1.PullIfNotPresent},
		{name: "digest reference with registry port", image: "localhost:5000/axonhub@sha256:abc123", want: corev1.PullIfNotPresent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pullPolicyFor(tt.image); got != tt.want {
				t.Errorf("pullPolicyFor(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestMergeLabels(t *testing.T) {
	dst := map[string]string{"other-actor/label": "keep", "app.kubernetes.io/name": "stale"}
	mergeLabels(dst, labelsForAxonHub("my-instance"))

	want := map[string]string{
		"other-actor/label":            "keep",
		"app.kubernetes.io/name":       "axonhub",
		"app.kubernetes.io/instance":   "my-instance",
		"app.kubernetes.io/managed-by": "axonhub-operator",
	}
	if len(dst) != len(want) {
		t.Fatalf("merged labels have %d entries, want %d: %v", len(dst), len(want), dst)
	}
	for k, v := range want {
		if dst[k] != v {
			t.Errorf("merged label %q = %q, want %q", k, dst[k], v)
		}
	}
}
