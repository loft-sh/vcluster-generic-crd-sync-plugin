package namecache

import (
	"fmt"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/patches"
	patchesregex "github.com/loft-sh/vcluster-generic-crd-plugin/pkg/patches/regex"
	"github.com/loft-sh/vcluster-sdk/translate"
	"github.com/pkg/errors"
	"github.com/vmware-labs/yaml-jsonpath/pkg/yamlpath"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type fromVirtualClusterCacheHandler struct {
	gvk       schema.GroupVersionKind
	mapping   *config.FromVirtualCluster
	nameCache *nameCache
}

func (c *fromVirtualClusterCacheHandler) OnAdd(obj interface{}) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if ok {
		newMappings, err := c.mappingsFromVirtualObject(unstructuredObj, c.mapping)
		if err == nil {
			fmt.Println(unstructuredObj.GetNamespace() + "/" + unstructuredObj.GetName())
			c.nameCache.ExchangeMapping(c.gvk, &IndexMappings{
				Name:     unstructuredObj.GetNamespace() + "/" + unstructuredObj.GetName(),
				Mappings: newMappings,
			})
		}
	}
}

func (c *fromVirtualClusterCacheHandler) OnUpdate(oldObj, newObj interface{}) {
	unstructuredObj, ok := newObj.(*unstructured.Unstructured)
	if ok {
		newMappings, err := c.mappingsFromVirtualObject(unstructuredObj, c.mapping)
		if err == nil {
			c.nameCache.ExchangeMapping(c.gvk, &IndexMappings{
				Name:     unstructuredObj.GetNamespace() + "/" + unstructuredObj.GetName(),
				Mappings: newMappings,
			})
		}
	}
}

func (c *fromVirtualClusterCacheHandler) OnDelete(obj interface{}) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if ok {
		c.nameCache.RemoveMapping(c.gvk, unstructuredObj.GetNamespace()+"/"+unstructuredObj.GetName())
	}
}

func (c *fromVirtualClusterCacheHandler) mappingsFromVirtualObject(obj *unstructured.Unstructured, mappingConfig *config.FromVirtualCluster) (map[string]map[string]string, error) {
	mappings := map[string]map[string]string{}
	mappings[IndexPhysicalToVirtualName] = map[string]string{}
	mappings[IndexPhysicalToVirtualNamePath] = map[string]string{}

	// add metadata.name mapping
	addSingleMapping(mappings, obj.GetNamespace()+"/"+obj.GetName(), translate.PhysicalName(obj.GetName(), obj.GetNamespace()), MetadataFieldPath)

	// TODO add explicit name caches?
	for _, p := range mappingConfig.Patches {
		if p.Operation != config.PatchTypeRewriteName {
			continue
		}

		node, err := patches.NewJSONNode(obj.Object)
		if err != nil {
			return nil, err
		}

		path, err := yamlpath.NewPath(p.Path)
		if err != nil {
			return nil, errors.Wrapf(err, "compile path %s", p.Path)
		}

		matches, err := path.Find(node)
		if err != nil {
			return nil, err
		}

		for _, m := range matches {
			if m.Kind == yaml.ScalarNode {

				if p.ParsedRegex != nil {
					_ = patchesregex.ProcessRegex(p.ParsedRegex, m.Value, func(name, namespace string) types.NamespacedName {
						// if the regex match doesn't contain namespace - use the namespace of the virtual object that is being handled
						if namespace == "" {
							namespace = obj.GetNamespace()
						}
						addSingleMapping(mappings, namespace+"/"+name, translate.PhysicalName(name, namespace), p.Path)

						// return empty as return value will not be used, we only want to add the mappings above
						return types.NamespacedName{}
					})
				} else {
					addSingleMapping(mappings, obj.GetNamespace()+"/"+m.Value, translate.PhysicalName(m.Value, obj.GetNamespace()), p.Path)
				}
			}
		}
	}

	return mappings, nil
}

func addSingleMapping(mappings map[string]map[string]string, virtualName, hostName, path string) {
	mappings[IndexPhysicalToVirtualName][hostName] = virtualName
	mappings[IndexPhysicalToVirtualNamePath][hostName+"/"+path] = virtualName
}
