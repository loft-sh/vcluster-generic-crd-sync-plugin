package namecache

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	MetadataFieldPath = "metadata.name"
)

const (
	IndexPhysicalToVirtualName     = "indexphysicaltovirtualname"
	IndexPhysicalToVirtualNamePath = "indexphysicaltovirtualnamepath"
)

type HookFunc func(name, key, value string)

type NameCache interface {
	GetFirstByIndex(gvk schema.GroupVersionKind, index, key string) string
	ResolveName(gvk schema.GroupVersionKind, hostName string) types.NamespacedName
	ResolveNamePath(gvk schema.GroupVersionKind, hostName string, path string) types.NamespacedName
	AddChangeHook(gvk schema.GroupVersionKind, index string, hookFunc HookFunc)

	ExchangeMapping(gvk schema.GroupVersionKind, object *IndexMappings)
	RemoveMapping(gvk schema.GroupVersionKind, name string)
}

func NewNameCache(ctx context.Context, manager ctrl.Manager, mappings *config.Config) (NameCache, error) {
	nc := &nameCache{
		indices: map[schema.GroupVersionKind]map[string]map[string][]*Object{},
		objects: map[schema.GroupVersionKind]map[string]*IndexMappings{},
		hooks:   map[schema.GroupVersionKind]map[string][]HookFunc{},
	}

	for _, mapping := range mappings.Mappings {
		if mapping.FromVirtualCluster != nil {
			// add informer to cache
			gvk := schema.FromAPIVersionAndKind(mapping.FromVirtualCluster.APIVersion, mapping.FromVirtualCluster.Kind)

			// check if there is at least 1 reverse patch that would use the cache
			found := false
			for _, p := range mapping.FromVirtualCluster.ReversePatches {
				if p.Operation == config.PatchTypeRewriteName {
					found = true
					break
				}
			}
			// check if there is any built-in sync enabled, as those use cache hooks
			for _, p := range mapping.FromVirtualCluster.Patches {
				if p.Sync != nil && ((p.Sync.Secret != nil && *p.Sync.Secret) || (p.Sync.ConfigMap != nil && *p.Sync.ConfigMap)) {
					found = true
					break
				}
			}
			if len(mapping.FromVirtualCluster.SyncBack) > 0 {
				found = true
			}
			if !found {
				continue
			}

			// construct object and watch
			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion(mapping.FromVirtualCluster.APIVersion)
			obj.SetKind(mapping.FromVirtualCluster.Kind)
			informer, err := manager.GetCache().GetInformer(ctx, obj)
			if err != nil {
				return nil, fmt.Errorf("get informer for %v: %v", gvk, err)
			}

			informer.AddEventHandler(&fromVirtualClusterCacheHandler{
				gvk:       gvk,
				mapping:   mapping.FromVirtualCluster,
				nameCache: nc,
			})
		} else {
			return nil, fmt.Errorf("currently expects fromVirtualCluster to be defined")
		}
	}

	return nc, nil
}

type nameCache struct {
	m sync.Mutex

	// GVK -> Index -> Lookup Key -> Object
	indices map[schema.GroupVersionKind]map[string]map[string][]*Object
	// GVK -> Name -> Mappings
	objects map[schema.GroupVersionKind]map[string]*IndexMappings
	// GVK -> Index -> Hooks
	hooks map[schema.GroupVersionKind]map[string][]HookFunc
}

type Object struct {
	// Name of the object this mapping was retrieved from
	Name string

	// Value this object maps to in the given index and lookup key
	Value string
}

type IndexMappings struct {
	// Name of the object this mapping was retrieved from
	Name string

	// Mappings maps the Index -> Lookup Key -> Value
	Mappings map[string]map[string]string
}

func StringToNamespacedName(n string) types.NamespacedName {
	nn := types.NamespacedName{}
	parts := strings.Split(n, "/")
	if len(parts) == 2 {
		nn.Namespace = parts[0]
		nn.Name = parts[1]
	}
	return nn
}

func (n *nameCache) GetByIndex(gvk schema.GroupVersionKind, index, key string) []*Object {
	n.m.Lock()
	defer n.m.Unlock()

	indicesMap, ok := n.indices[gvk]
	if !ok {
		return nil
	}

	keysMap, ok := indicesMap[index]
	if !ok {
		return nil
	}

	return keysMap[key]
}

func (n *nameCache) GetFirstByIndex(gvk schema.GroupVersionKind, index, key string) string {
	objects := n.GetByIndex(gvk, index, key)
	if len(objects) == 0 {
		return ""
	}

	return objects[0].Value
}

func (n *nameCache) ResolveName(gvk schema.GroupVersionKind, hostName string) types.NamespacedName {
	value := n.GetFirstByIndex(gvk, IndexPhysicalToVirtualName, hostName)
	if value == "" {
		return types.NamespacedName{}
	}

	return StringToNamespacedName(value)
}

func (n *nameCache) ResolveNamePath(gvk schema.GroupVersionKind, hostName, fieldPath string) types.NamespacedName {
	value := n.GetFirstByIndex(gvk, IndexPhysicalToVirtualNamePath, hostName+"/"+fieldPath)
	if value == "" {
		return types.NamespacedName{}
	}

	return StringToNamespacedName(value)
}

func (n *nameCache) RemoveMapping(gvk schema.GroupVersionKind, name string) {
	n.m.Lock()
	defer n.m.Unlock()

	n.removeMapping(gvk, name)
}

func (n *nameCache) removeMapping(gvk schema.GroupVersionKind, name string) {
	objectsMap, ok := n.objects[gvk]
	if !ok || objectsMap == nil {
		return
	}

	mappings, ok := objectsMap[name]
	if !ok || mappings == nil {
		return
	}

	// make sure object is deleted
	delete(objectsMap, name)

	// delete the mappings
	for index, mappingKeyValues := range mappings.Mappings {
		for mappingKey, mappingValue := range mappingKeyValues {
			indexMappings, ok := n.indices[gvk]
			if len(indexMappings) == 0 || !ok {
				continue
			}

			keyValueMappings, ok := indexMappings[index]
			if len(keyValueMappings) == 0 || !ok {
				continue
			}

			objectMappings, ok := keyValueMappings[mappingKey]
			if !ok || len(objectMappings) == 0 {
				continue
			} else if len(objectMappings) == 1 {
				delete(n.indices[gvk][index], mappingKey)
			}

			otherMappings := []*Object{}
			for _, objectMapping := range objectMappings {
				if objectMapping.Name == name {
					continue
				}

				otherMappings = append(otherMappings, objectMapping)
			}
			n.indices[gvk][index][mappingKey] = otherMappings

			// execute hooks for this index
			n.executeHooks(gvk, index, name, mappingKey, mappingValue)
		}
	}
}

func (n *nameCache) ExchangeMapping(gvk schema.GroupVersionKind, object *IndexMappings) {
	n.m.Lock()
	defer n.m.Unlock()

	if n.objects[gvk] == nil {
		n.objects[gvk] = map[string]*IndexMappings{}
	}

	oldObject, ok := n.objects[gvk][object.Name]
	if ok && equality.Semantic.DeepEqual(object, oldObject) {
		return
	} else if ok {
		// remove
		n.removeMapping(gvk, object.Name)
	}

	// add
	if len(object.Mappings) > 0 {
		n.objects[gvk][object.Name] = object
	}
	if n.indices[gvk] == nil {
		n.indices[gvk] = map[string]map[string][]*Object{}
	}

	// add index values
	for index, mappingKeyValues := range object.Mappings {
		if len(mappingKeyValues) == 0 {
			continue
		}
		if n.indices[gvk][index] == nil {
			n.indices[gvk][index] = map[string][]*Object{}
		}

		for key, value := range mappingKeyValues {
			values := n.indices[gvk][index][key]
			values = append(values, &Object{
				Name:  object.Name,
				Value: value,
			})
			n.indices[gvk][index][key] = values

			// execute hooks for this index
			n.executeHooks(gvk, index, object.Name, key, value)
		}
	}
}

func (n *nameCache) executeHooks(gvk schema.GroupVersionKind, index string, name, key, value string) {
	gvkHooks, ok := n.hooks[gvk]
	if !ok {
		return
	}

	hooks, ok := gvkHooks[index]
	if !ok {
		return
	}

	for _, hook := range hooks {
		hook(name, key, value)
	}
}

func (n *nameCache) AddChangeHook(gvk schema.GroupVersionKind, index string, hookFunc HookFunc) {
	n.m.Lock()
	defer n.m.Unlock()

	if n.hooks[gvk] == nil {
		n.hooks[gvk] = map[string][]HookFunc{}
	}
	if n.hooks[gvk][index] == nil {
		n.hooks[gvk][index] = []HookFunc{}
	}
	n.hooks[gvk][index] = append(n.hooks[gvk][index], hookFunc)
}
