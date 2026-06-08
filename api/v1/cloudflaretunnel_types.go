package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// CloudflareTunnelSpec defines the desired state of CloudflareTunnel
type CloudflareTunnelSpec struct {
	// Hostname is the public DNS hostname (e.g. app.example.com)
	Hostname string `json:"hostname"`

	// Rules defines how incoming requests are routed to Kubernetes services
	Rules []TunnelRule `json:"rules"`

	// Replicas is the number of cloudflared pods to run (default 2)
	Replicas *int32 `json:"replicas,omitempty"`
}

// TunnelRule maps a path to a backend service
type TunnelRule struct {
	Path    string          `json:"path"`
	Backend ServiceBackend  `json:"backend"`
}

// ServiceBackend references a Kubernetes Service
type ServiceBackend struct {
	ServiceName string `json:"serviceName"`
	Port        int32  `json:"port"`
}

// CloudflareTunnelStatus defines the observed state of CloudflareTunnel
type CloudflareTunnelStatus struct {
	TunnelID string `json:"tunnelID,omitempty"`
	Ready    bool   `json:"ready,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CloudflareTunnel is the Schema for the cloudflaretunnels API
type CloudflareTunnel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudflareTunnelSpec   `json:"spec,omitempty"`
	Status CloudflareTunnelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudflareTunnelList contains a list of CloudflareTunnel
type CloudflareTunnelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudflareTunnel `json:"items"`
}

// DeepCopyInto copies the receiver into out.
func (in *CloudflareTunnel) DeepCopyInto(out *CloudflareTunnel) {
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Spec.Rules = make([]TunnelRule, len(in.Spec.Rules))
	copy(out.Spec.Rules, in.Spec.Rules)
	out.Status = in.Status
}

// DeepCopy returns a deep copy.
func (in *CloudflareTunnel) DeepCopy() *CloudflareTunnel {
	if in == nil {
		return nil
	}
	out := new(CloudflareTunnel)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *CloudflareTunnel) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies the receiver into out.
func (in *CloudflareTunnelList) DeepCopyInto(out *CloudflareTunnelList) {
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]CloudflareTunnel, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy returns a deep copy.
func (in *CloudflareTunnelList) DeepCopy() *CloudflareTunnelList {
	if in == nil {
		return nil
	}
	out := new(CloudflareTunnelList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *CloudflareTunnelList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func init() {
	SchemeBuilder.Register(&CloudflareTunnel{}, &CloudflareTunnelList{})
}
