package namecache

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/pkg/errors"
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

type HookFunc func(types.NamespacedName)

type NameCache interface {
	ResolveName(hostName string, path string) types.NamespacedName
	ResolveHostName(virtualName types.NamespacedName, path string) string
	AddPathChangeHook(path string, hookFunc HookFunc)
}

func NewNameCache(ctx context.Context, manager ctrl.Manager, mapping *config.Mapping) (NameCache, error) {
	nc := &nameCache{
		manager:            manager,
		hostToVirtualNames: map[string]map[string][]*virtualObject{},
		virtualObjects:     map[string]*virtualObject{},
		pathHooks:          map[string][]HookFunc{},
	}

	if mapping.FromVirtualCluster != nil {
		found := false

		// check if there is at least 1 reverse patch that would use the cache
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

		// add informer to cache
		gvk := schema.FromAPIVersionAndKind(mapping.FromVirtualCluster.ApiVersion, mapping.FromVirtualCluster.Kind)
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
	} else if mapping.FromHostCluster != nil {
		// check if there is at least 1 mapping
		if mapping.FromHostCluster.NameMapping.RewriteName != config.RewriteNameTypeFromHostToVirtualNamespace {
			// check if there is a patch that rewrites a name
			found := false
			for _, p := range mapping.FromVirtualCluster.Patches {
				if p.Operation == config.RewriteNameTypeFromHostToVirtualNamespace {
					found = true
					break
				}
			}
			if !found {
				return nc, nil
			}
		}

		// add informer to cache
		gvk := schema.FromAPIVersionAndKind(mapping.FromHostCluster.ApiVersion, mapping.FromHostCluster.Kind)
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(mapping.FromHostCluster.ApiVersion)
		obj.SetKind(mapping.FromHostCluster.Kind)
		informer, err := nc.manager.GetCache().GetInformer(ctx, obj)
		if err != nil {
			return nil, errors.Wrapf(err, "get informer for %v", gvk)
		}

		informer.AddEventHandler(&fromHostClusterCacheHandler{
			gvk:       gvk,
			mapping:   mapping.FromVirtualCluster,
			nameCache: nc,
		})
	}

	return nc, nil
}

type nameCache struct {
	manager ctrl.Manager

	m                  sync.Mutex
	hostToVirtualNames map[string]map[string][]*virtualObject
	virtualObjects     map[string]*virtualObject
	pathHooks          map[string][]HookFunc
}

type virtualObject struct {
	GVK         schema.GroupVersionKind
	VirtualName string

	Mappings []mapping
}

type mapping struct {
	// VirtualName holds the name in format Namespace/Name
	VirtualName string
	// HostName holds the generated name in format Name
	HostName string
	// Path to the field used for creating this mapping
	FieldPath string
}

func (v virtualObject) String() string {
	return v.VirtualName + "/" + v.GVK.String()
}

func stringToNamespacedName(n string) types.NamespacedName {
	nn := types.NamespacedName{}
	parts := strings.Split(n, "/")
	if len(parts) == 2 {
		nn.Namespace = parts[0]
		nn.Name = parts[1]
	}
	return nn
}

func (n *nameCache) ResolveName(hostName string, fieldPath string) types.NamespacedName {
	n.m.Lock()
	defer n.m.Unlock()

	if len(n.hostToVirtualNames[fieldPath]) > 0 {
		slice := n.hostToVirtualNames[fieldPath][hostName]
		if len(slice) > 0 {
			for _, virtualObject := range slice {
				for _, m := range virtualObject.Mappings {
					if m.HostName == hostName {
						return stringToNamespacedName(m.VirtualName)
					}
				}
			}
		}
	}

	return types.NamespacedName{}
}

// ResolveHostName returns HostName of a mapping based on the fieldPath and VirtualName of the mapping
func (n *nameCache) ResolveHostName(virtualName types.NamespacedName, fieldPath string) string {
	vName := virtualName.Namespace + "/" + virtualName.Name
	n.m.Lock()
	defer n.m.Unlock()

	// TODO: improve data structure used in the cache so we can avoid brute force search below
	for _, virtualObject := range n.virtualObjects {
		for _, m := range virtualObject.Mappings {
			if m.VirtualName == vName && m.FieldPath == fieldPath {
				return m.HostName
			}
		}
	}

	return ""
}

func (n *nameCache) RemoveMapping(object *virtualObject) {
	n.m.Lock()
	defer n.m.Unlock()

	n.removeMapping(object)
}

func (n *nameCache) removeMapping(object *virtualObject) {
	name := object.String()
	oldVirtualObject, ok := n.virtualObjects[name]
	if ok {
		delete(n.virtualObjects, name)

		for _, mapping := range oldVirtualObject.Mappings {
			if len(n.hostToVirtualNames[mapping.FieldPath]) == 0 {
				continue
			}
			slice, ok := n.hostToVirtualNames[mapping.FieldPath][mapping.HostName]
			if ok {
				if len(slice) == 0 {
					delete(n.hostToVirtualNames[mapping.FieldPath], mapping.HostName)
					continue
				} else if len(slice) == 1 && slice[0].String() == name {
					delete(n.hostToVirtualNames[mapping.FieldPath], mapping.HostName)
					continue
				}

				otherObjects := []*virtualObject{}
				for _, oldObject := range slice {
					if oldObject.String() == name {
						continue
					}
					otherObjects = append(otherObjects, oldObject)
				}
				if len(slice) == 0 {
					delete(n.hostToVirtualNames[mapping.FieldPath], mapping.HostName)
					continue
				}
				n.hostToVirtualNames[mapping.FieldPath][mapping.HostName] = otherObjects
			}
		}
	}
}

func (n *nameCache) exchangeMapping(object *virtualObject) {
	n.m.Lock()
	defer n.m.Unlock()

	name := object.String()
	oldVirtualObject, ok := n.virtualObjects[name]
	if ok && equality.Semantic.DeepEqual(object, oldVirtualObject) {
		return
	} else if ok {
		// remove
		n.removeMapping(object)
	}

	// add
	if len(object.Mappings) > 0 {
		n.virtualObjects[object.String()] = object
		for _, m := range object.Mappings {
			if len(n.hostToVirtualNames[m.FieldPath]) == 0 {
				n.hostToVirtualNames[m.FieldPath] = map[string][]*virtualObject{}
			}
			slice, ok := n.hostToVirtualNames[m.FieldPath][m.HostName]
			if ok {
				slice = append(slice, object)
				n.hostToVirtualNames[m.FieldPath][m.HostName] = slice
			} else {
				n.hostToVirtualNames[m.FieldPath][m.HostName] = []*virtualObject{object}
			}

			// execute hooks for the mappings that had a change to virtual name
			executeHooks := true
			if oldVirtualObject != nil {
				for _, om := range oldVirtualObject.Mappings {
					if m.FieldPath == om.FieldPath {
						executeHooks = m.VirtualName != om.VirtualName
						break
					}
				}
			}
			if executeHooks {
				for _, hook := range n.pathHooks[m.FieldPath] {
					hook(stringToNamespacedName(m.VirtualName))
				}
			}
		}
	}
}

func (n *nameCache) AddPathChangeHook(path string, hookFunc HookFunc) {
	n.pathHooks[path] = append(n.pathHooks[path], hookFunc)
}
