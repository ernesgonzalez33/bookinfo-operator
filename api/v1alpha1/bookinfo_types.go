/*
Copyright 2023.

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

// BookinfoSpec defines the desired state of Bookinfo
type BookinfoSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Replicas                  int32  `json:"replicas,omitempty"`
	MeshEnabled               bool   `json:"meshEnabled"`
	MeshControlPlaneName      string `json:"meshControlPlaneName,omitempty"`
	MeshControlPlaneNamespace string `json:"meshControlPlaneNamespace,omitempty"`
}

// BookinfoStatus defines the observed state of Bookinfo
type BookinfoStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Replicas                  int32  `json:"replicas,omitempty"`
	MeshEnabled               bool   `json:"meshEnabled"`
	MeshControlPlaneName      string `json:"meshControlPlaneName,omitempty"`
	MeshControlPlaneNamespace string `json:"meshControlPlaneNamespace,omitempty"`

	// Conditions store the status conditions of the Memcached instances
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Bookinfo is the Schema for the bookinfoes API
type Bookinfo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BookinfoSpec   `json:"spec,omitempty"`
	Status BookinfoStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BookinfoList contains a list of Bookinfo
type BookinfoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Bookinfo `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Bookinfo{}, &BookinfoList{})
}
