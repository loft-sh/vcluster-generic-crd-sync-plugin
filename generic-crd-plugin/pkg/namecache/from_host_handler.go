package namecache

import (
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
)

type fromHostClusterCacheHandler struct {
	gvk       schema.GroupVersionKind
	mapping   *config.FromVirtualCluster
	nameCache *nameCache
}

func (c *fromHostClusterCacheHandler) OnAdd(obj interface{}) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if ok {
		newMappings, err := c.mappingsFromVirtualObject(unstructuredObj)
		if err == nil {
			c.nameCache.exchangeMapping(&virtualObject{
				GVK:         c.gvk,
				VirtualName: unstructuredObj.GetNamespace() + "/" + unstructuredObj.GetName(),
				Mappings:    newMappings,
			})
		}
	}
}

func (c *fromHostClusterCacheHandler) OnUpdate(oldObj, newObj interface{}) {
	unstructuredObj, ok := newObj.(*unstructured.Unstructured)
	if ok {
		newMappings, err := c.mappingsFromVirtualObject(unstructuredObj)
		if err == nil {
			c.nameCache.exchangeMapping(&virtualObject{
				GVK:         c.gvk,
				VirtualName: unstructuredObj.GetNamespace() + "/" + unstructuredObj.GetName(),
				Mappings:    newMappings,
			})
		}
	}
}

func (c *fromHostClusterCacheHandler) OnDelete(obj interface{}) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if ok {
		c.nameCache.removeMapping(&virtualObject{
			GVK:         c.gvk,
			VirtualName: unstructuredObj.GetNamespace() + "/" + unstructuredObj.GetName(),
		})
	}
}

func (c *fromHostClusterCacheHandler) mappingsFromVirtualObject(obj *unstructured.Unstructured) ([]mapping, error) {
	mappings := []mapping{}
	annotations := obj.GetAnnotations()
	if annotations != nil && annotations[MappingAnnotation] != "" {
		mappingStrings := strings.Split(annotations[MappingAnnotation], "\n")
		for _, s := range mappingStrings {
			splitted := strings.Split(s, "=")
			if len(splitted) == 2 {
				mappings = append(mappings, mapping{
					VirtualName: splitted[0],
					HostName:    splitted[1],
				})
			}
		}
	}

	return mappings, nil
}
