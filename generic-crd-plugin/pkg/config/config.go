package config

const Version = "v1beta1"

type Config struct {
	// Version is the config version
	Version string `json:"version,omitempty"`

	// Mappings defines a way to map a resource to another resource
	Mappings []Mapping `json:"mappings,omitempty"`
}

type Mapping struct {
	// FromHost syncs a resource from the host to the virtual cluster
	FromHostCluster *FromHostCluster `json:"fromHostCluster,omitempty"`

	// FromVirtualCluster syncs a resource from the virtual cluster to the host
	FromVirtualCluster *FromVirtualCluster `json:"fromVirtualCluster,omitempty"`
}

type FromVirtualCluster struct {
	TypeInformation `json:",inline"`

	// NameMapping defines how objects will be mapped between host and
	// virtual cluster.
	NameMapping NameMapping `json:"nameMapping,omitempty"`

	// Selector is the selector to select the objects in the host cluster. If empty
	// will select all objects.
	Selector *Selector `json:"selector,omitempty"`

	// Patches are the patches to apply on the host cluster objects
	Patches []Patch `json:"patches,omitempty"`
}

type FromHostCluster struct {
	TypeInformation `json:",inline"`

	// NameMapping defines how objects will be mapped between host and
	// virtual cluster.
	NameMapping NameMapping `json:"nameMapping,omitempty"`

	// Selector is the selector to select the objects in the host cluster. If empty
	// will select all objects.
	Selector *Selector `json:"selector,omitempty"`

	// Patches are the patches to apply on the host cluster objects
	Patches []Patch `json:"patches,omitempty"`
}

type TypeInformation struct {
	ApiVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`

	// ClusterScoped determines if the objects are cluster scoped
	ClusterScoped bool `json:"clusterScoped,omitempty"`
}

type NameMapping struct {
	// RewriteName defines
	RewriteName RewriteNameType `json:"rewriteName,omitempty"`

	// Namespace allows you to define a namespace the objects should get written to
	// if policy is RewriteNameTypeKeepName
	Namespace string
}

type RewriteNameType string

const (
	RewriteNameTypeKeepName                   = "KeepName"
	RewriteNameTypeFromVirtualToHostNamespace = "FromVirtualToHostNamespace"
	RewriteNameTypeFromHostToVirtualNamespace = "FromHostToVirtualNamespace"
)

type Selector struct {
	// LabelSelector are the labels to select the object from
	LabelSelector map[string]string `json:"labelSelector,omitempty"`
}

type Patch struct {
	// Type is the type of the patch
	Type PatchType `yaml:"type,omitempty" json:"type,omitempty"`

	// Path is the path of the patch
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Value is the value of the path
	Value interface{} `yaml:"value,omitempty" json:"value,omitempty"`

	// Conditions are conditions that must be true for
	// the patch to get executed
	Conditions []PatchCondition `yaml:"conditions,omitempty" json:"conditions,omitempty"`
}

type PatchType string

const (
	PatchTypeRewriteNameFromVirtualToHostNamespace = "RewriteFromVirtualToHostNamespace"
	PatchTypeRewriteNameFromHostToVirtualNamespace = "RewriteFromHostToVirtualNamespace"
)

type PatchCondition struct {
	// Path is the path within the object to select
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// SubPath is the path below the selected object to select
	SubPath string `yaml:"subPath,omitempty" json:"subPath,omitempty"`

	// Equal is the value the path should be equal to
	Equal interface{} `yaml:"equal,omitempty" json:"equal,omitempty"`

	// NotEqual is the value the path should not be equal to
	NotEqual interface{} `yaml:"notEqual,omitempty" json:"notEqual,omitempty"`

	// Empty means that the path value should be empty or unset
	Empty bool `yaml:"empty,omitempty" json:"empty,omitempty"`
}
