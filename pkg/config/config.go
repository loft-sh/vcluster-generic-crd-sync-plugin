package config

import "regexp"

const Version = "v1beta1"

type Config struct {
	// Version is the config version
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	// Mappings defines a way to map a resource to another resource
	Mappings []Mapping `yaml:"mappings,omitempty" json:"mappings,omitempty"`
}

type Mapping struct {
	// FromHost syncs a resource from the host to the virtual cluster
	FromHostCluster *FromHostCluster `yaml:"fromHostCluster,omitempty" json:"fromHostCluster,omitempty"`

	// FromVirtualCluster syncs a resource from the virtual cluster to the host
	FromVirtualCluster *FromVirtualCluster `yaml:"fromVirtualCluster,omitempty" json:"fromVirtualCluster,omitempty"`
}

type SyncBase struct {
	TypeInformation `yaml:",inline" json:",inline"`

	// ID is the id of the controller. This is optional and only necessary if you have multiple syncBack or fromVirtualSyncer
	// controllers that target the same group version kind.
	ID string `yaml:"id,omitempty" json:"name,omitempty"`

	// Patches are the patches to apply on the virtual cluster objects
	// when syncing them from the host cluster
	Patches []*Patch `yaml:"patches,omitempty" json:"patches,omitempty"`

	// ReversePatches are the patches to apply to host cluster objects
	// after it has been synced to the virtual cluster
	ReversePatches []*Patch `yaml:"reversePatches,omitempty" json:"reversePatches,omitempty"`
}

type FromVirtualCluster struct {
	SyncBase `yaml:",inline" json:",inline"`

	// Selector is the selector to select the objects in the host cluster.
	// If empty will select all objects.
	Selector *Selector `yaml:"selector,omitempty" json:"selector,omitempty"`

	// Resources to sync back to virtual cluster
	SyncBack []*SyncBack `yaml:"syncBack,omitempty" json:"syncBack,omitempty"`
}

type SyncBack struct {
	SyncBase `yaml:",inline" json:",inline"`

	// Selectors are the SyncBackSelector definitions to select the objects
	// in the host cluster thatwill be synced to the virtual cluster
	// If empty will select all objects.
	Selectors []*SyncBackSelector `yaml:"selectors,omitempty" json:"selectors,omitempty"`
}

type SyncBackSelector struct {
	// Select object to sync based on its .metadata.name
	Name *NameSyncBackSelector `yaml:"name,omitempty" json:"name,omitempty"`
}

type NameSyncBackSelector struct {
	// Path to a field of parent sync object that references name of the resources
	// that we wish to sync back to the virtual cluster
	RewrittenPath string `yaml:"rewrittenPath,omitempty" json:"rewrittenPath,omitempty"`
}

type FromHostCluster struct {
	SyncBase `yaml:",inline" json:",inline"`

	// NameMapping defines how objects will be mapped between host and
	// virtual cluster.
	NameMapping NameMapping `yaml:"nameMapping,omitempty" json:"nameMapping,omitempty"`

	// Selector is the selector to select the objects in the host cluster.
	// If empty will select all objects.
	Selector *Selector `yaml:"selector,omitempty" json:"selector,omitempty"`
}

type TypeInformation struct {
	// APIVersion of the object to sync
	APIVersion string `yaml:"apiVersion,omitempty" json:"apiVersion,omitempty"`

	// Kind of the object to sync
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
}

type NameMapping struct {
	// RewriteName defines
	RewriteName RewriteNameType `yaml:"rewriteName,omitempty" json:"rewriteName,omitempty"`

	// Namespace allows you to define a namespace the objects should get written to
	// if policy is RewriteNameTypeKeepName
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

type RewriteNameType string

const (
	RewriteNameTypeKeepName                   = "KeepName"
	RewriteNameTypeFromVirtualToHostNamespace = "FromVirtualToHostNamespace"
	RewriteNameTypeFromHostToVirtualNamespace = "FromHostToVirtualNamespace"
)

type Selector struct {
	// LabelSelector are the labels to select the object from
	LabelSelector map[string]string `yaml:"labelSelector,omitempty" json:"labelSelector,omitempty"`
}

type Patch struct {
	// Operation is the type of the patch
	Operation PatchType `yaml:"op,omitempty" json:"op,omitempty"`

	// FromPath is the path from the other object
	FromPath string `yaml:"fromPath,omitempty" json:"fromPath,omitempty"`

	// Path is the path of the patch
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// NamePath is the path to the name of a child resource within Path
	NamePath string `yaml:"namePath,omitempty" json:"namePath,omitempty"`

	// NamespacePath is path to the namespace of a child resource within Path
	NamespacePath string `yaml:"namespacePath,omitempty" json:"namespacePath,omitempty"`

	// Value is the new value to be set to the path
	Value interface{} `yaml:"value,omitempty" json:"value,omitempty"`

	// Regex - is regular expresion used to identify the Name,
	// and optionally Namespace, parts of the field value that
	// will be replaced with the rewritten Name and/or Namespace
	Regex       string         `yaml:"regex,omitempty" json:"regex,omitempty"`
	ParsedRegex *regexp.Regexp `yaml:"-" json:"-"`

	// Conditions are conditions that must be true for
	// the patch to get executed
	Conditions []*PatchCondition `yaml:"conditions,omitempty" json:"conditions,omitempty"`

	// Ignore determines if the path should be ignored if handled as a reverse patch
	Ignore *bool `yaml:"ignore,omitempty" json:"ignore,omitempty"`

	// Sync defines if a specialized syncer should be initialized using values
	// from the rewriteName operation as Secret/Confgimap names to be synced
	Sync *PatchSync `yaml:"sync,omitempty" json:"sync,omitempty"`
}

type PatchType string

const (
	PatchTypeRewriteName                     = "rewriteName"
	PatchTypeRewriteLabelSelector            = "rewriteLabelSelector"
	PatchTypeRewriteLabelExpressionsSelector = "rewriteLabelExpressionsSelector"

	PatchTypeCopyFromObject = "copyFromObject"
	PatchTypeAdd            = "add"
	PatchTypeReplace        = "replace"
	PatchTypeRemove         = "remove"
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
	Empty *bool `yaml:"empty,omitempty" json:"empty,omitempty"`
}
type PatchSync struct {
	Secret    *bool `yaml:"secret,omitempty" json:"secret,omitempty"`
	ConfigMap *bool `yaml:"configmap,omitempty" json:"configmap,omitempty"`
}
