package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PostgresInstanceSpec struct {
	Version string `json:"version"`

	Replicas int `json:"replicas,omitempty"`

	Storage StorageSpec `json:"storage"`

	Resources *ResourceSpec `json:"resources,omitempty"`

	Backup *BackupSpec `json:"backup,omitempty"`

	Databases []DatabaseSpec `json:"databases,omitempty"`

	Users []UserSpec `json:"users,omitempty"`
}

type StorageSpec struct {
	Size string `json:"size"`

	StorageClass string `json:"storageClass,omitempty"`
}

type ResourceSpec struct {
	Requests ResourceList `json:"requests,omitempty"`

	Limits ResourceList `json:"limits,omitempty"`
}

type ResourceList struct {
	CPU string `json:"cpu,omitempty"`

	Memory string `json:"memory,omitempty"`
}

type BackupSpec struct {
	Enabled bool `json:"enabled"`

	Schedule string `json:"schedule,omitempty"`

	Retention int `json:"retention,omitempty"`

	S3 *S3Spec `json:"s3,omitempty"`
}

type S3Spec struct {
	Bucket string `json:"bucket"`

	Endpoint string `json:"endpoint,omitempty"`

	Region string `json:"region,omitempty"`
}

type DatabaseSpec struct {
	Name string `json:"name"`

	Owner string `json:"owner,omitempty"`
}

type UserSpec struct {
	Name string `json:"name"`

	Databases []string `json:"databases,omitempty"`
}

type PostgresInstanceStatus struct {
	Phase string `json:"phase,omitempty"`

	Ready bool `json:"ready,omitempty"`

	Message string `json:"message,omitempty"`

	LastBackup *metav1.Time `json:"lastBackup,omitempty"`

	Endpoints EndpointsSpec `json:"endpoints,omitempty"`
}

type EndpointsSpec struct {
	Primary string `json:"primary,omitempty"`

	Replicas []string `json:"replicas,omitempty"`
}

type PostgresInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostgresInstanceSpec   `json:"spec,omitempty"`
	Status PostgresInstanceStatus `json:"status,omitempty"`
}

type PostgresInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PostgresInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PostgresInstance{}, &PostgresInstanceList{})
}
