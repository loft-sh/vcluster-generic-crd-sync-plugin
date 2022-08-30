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
	MappingAnnotation = "vcluster.loft.sh/name-mappings"
	MetadataFieldPath = "metadata.name"
)

const (
	IndexPhysicalToVirtualName     = "indexphysicaltovirtualname"
	IndexPhysicalToVirtualNamePath = "indexphysicaltovirtualnamepath"
	IndexVirtualToPhysicalName     = "indexvirtualtophysicalname"
	IndexVirtualToPhysicalNamePath = "indexvirtualtophysicalnamepath"
)

type HookFunc func(name, key, value string)

type NameCache interface {
	GetFirstByIndex(index, key string) string
	ResolveName(hostName string) types.NamespacedName
	ResolveNamePath(hostName string, path string) types.NamespacedName
	ResolveHostName(virtualName types.NamespacedName) string
	ResolveHostNamePath(virtualName types.NamespacedName, path string) string
	AddChangeHook(index string, hookFunc HookFunc)
}

func NewNameCache(ctx context.Context, manager ctrl.Manager, mapping *config.Mapping) (NameCache, error) {
	nc := &nameCache{
		manager: manager,
		indices: map[schema.GroupVersionKind]map[string]map[string][]*Object{},
		objects: map[schema.GroupVersionKind]map[string]*indexMappings{},
		hooks:   map[schema.GroupVersionKind]map[string][]HookFunc{},
	}

	if mapping.FromVirtualCluster != nil {
		// add informer to cache
		gvk := schema.FromAPIVersionAndKind(mapping.FromVirtualCluster.ApiVersion, mapping.FromVirtualCluster.Kind)

		// check if there is at least 1 reverse patch that would use the cache
		found := false
		for _, p := range mapping.FromVirtualCluster.ReversePatches {
			if p.Operation == config.PatchTypeRewriteName || p.Operation == config.PatchTypeRewriteNamespace {
				found = true
				break
			}
		}
		// TODO: add checks of syncBack - Phase 2
		if !found {
			return nc, nil
		}

		// construct object and watch
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(mapping.FromVirtualCluster.ApiVersion)
		obj.SetKind(mapping.FromVirtualCluster.Kind)
		informer, err := nc.manager.GetCache().GetInformer(ctx, obj)
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

	return nc, nil
}

type nameCache struct {
	manager ctrl.Manager
	m       sync.Mutex

	// TODO: gvk is currently a private member, in future we probably want to allow multiple
	// gvk's the user then can choose from.
	gvk schema.GroupVersionKind

	// GVK -> Index -> Lookup Key -> Object
	indices map[schema.GroupVersionKind]map[string]map[string][]*Object
	// GVK -> Name -> Mappings
	objects map[schema.GroupVersionKind]map[string]*indexMappings
	// GVK -> Index -> Hooks
	hooks map[schema.GroupVersionKind]map[string][]HookFunc
}

type Object struct {
	// Name of the object this mapping was retrieved from
	Name string

	// Value this object maps to in the given index and lookup key
	Value string
}

type indexMappings struct {
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

func (n *nameCache) GetFirstByIndex(index, key string) string {
	objects := n.GetByIndex(n.gvk, index, key)
	if len(objects) == 0 {
		return ""
	}

	return objects[0].Value
}

func (n *nameCache) ResolveName(hostName string) types.NamespacedName {
	value := n.GetFirstByIndex(IndexPhysicalToVirtualName, hostName)
	if value == "" {
		return types.NamespacedName{}
	}

	return StringToNamespacedName(value)
}

func (n *nameCache) ResolveNamePath(hostName, fieldPath string) types.NamespacedName {
	value := n.GetFirstByIndex(IndexPhysicalToVirtualNamePath, hostName+"/"+fieldPath)
	if value == "" {
		return types.NamespacedName{}
	}

	return StringToNamespacedName(value)
}

func (n *nameCache) ResolveHostName(virtualName types.NamespacedName) string {
	vName := virtualName.Namespace + "/" + virtualName.Name
	return n.GetFirstByIndex(IndexVirtualToPhysicalName, vName)
}

func (n *nameCache) ResolveHostNamePath(virtualName types.NamespacedName, fieldPath string) string {
	vName := virtualName.Namespace + "/" + virtualName.Name + "/" + fieldPath
	return n.GetFirstByIndex(IndexVirtualToPhysicalNamePath, vName)
}

func (n *nameCache) RemoveMapping(name string) {
	n.m.Lock()
	defer n.m.Unlock()

	n.removeMapping(name)
}

func (n *nameCache) removeMapping(name string) {
	gvk := n.gvk
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

func (n *nameCache) exchangeMapping(object *indexMappings) {
	n.m.Lock()
	defer n.m.Unlock()

	gvk := n.gvk
	if n.objects[gvk] == nil {
		n.objects[gvk] = map[string]*indexMappings{}
	}

	oldObject, ok := n.objects[gvk][object.Name]
	if ok && equality.Semantic.DeepEqual(object, oldObject) {
		return
	} else if ok {
		// remove
		n.removeMapping(object.Name)
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

func (n *nameCache) AddChangeHook(index string, hookFunc HookFunc) {
	n.m.Lock()
	defer n.m.Unlock()

	gvk := n.gvk
	if n.hooks[gvk] == nil {
		n.hooks[gvk] = map[string][]HookFunc{}
	}
	if n.hooks[gvk][index] == nil {
		n.hooks[gvk][index] = []HookFunc{}
	}
	n.hooks[gvk][index] = append(n.hooks[gvk][index], hookFunc)
}
