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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AxonHubSpec defines the desired state of AxonHub.
type AxonHubSpec struct {
	// Replicas is the desired number of AxonHub pods.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Image is the AxonHub container image (repository[:tag]).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:default="looplj/axonhub:latest"
	Image string `json:"image,omitempty"`
}

// AxonHubStatus defines the observed state of AxonHub.
type AxonHubStatus struct {
	// ObservedGeneration is the generation most recently observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ReadyReplicas is the number of pods of the managed Deployment reported ready.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Conditions represent the latest available observations of the resource's state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AxonHub is the Schema for the axonhubs API.
type AxonHub struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AxonHubSpec   `json:"spec,omitempty"`
	Status AxonHubStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AxonHubList contains a list of AxonHub.
type AxonHubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AxonHub `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AxonHub{}, &AxonHubList{})
}
