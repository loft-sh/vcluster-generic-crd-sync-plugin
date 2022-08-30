package patches

import (
	"encoding/json"
	"fmt"

	jsonyaml "github.com/ghodss/yaml"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NameResolver interface {
	TranslateName(name string, path string) (string, error)
}

func ApplyPatches(obj1, obj2 client.Object, patchesConf []*config.Patch, reversePatchesConf []*config.Patch, nameResolver NameResolver) error {
	node1, err := NewJSONNode(obj1)
	if err != nil {
		return errors.Wrap(err, "new json yaml node")
	}

	var node2 *yaml.Node
	if obj2 != nil {
		node2, err = NewJSONNode(obj2)
		if err != nil {
			return errors.Wrap(err, "new json yaml node")
		}
	}

	for _, p := range patchesConf {
		err := applyPatch(node1, node2, p, nameResolver)
		if err != nil {
			return errors.Wrap(err, "apply patch")
		}
	}

	// remove ignore paths from patched object
	for _, p := range reversePatchesConf {
		if p.Path == "" || (p.Ignore != nil && *p.Ignore == false) {
			continue
		}

		err := applyPatch(node1, node2, &config.Patch{
			Operation: config.PatchTypeRemove,
			Path:      p.Path,
		}, nameResolver)
		if err != nil {
			return errors.Wrap(err, "apply patch")
		}
	}

	objYaml, err := yaml.Marshal(node1)
	if err != nil {
		return errors.Wrap(err, "marshal yaml")
	}

	err = jsonyaml.Unmarshal(objYaml, obj1)
	if err != nil {
		return errors.Wrap(err, "convert object")
	}

	return nil
}

func applyPatch(obj1, obj2 *yaml.Node, patch *config.Patch, resolver NameResolver) error {
	if patch.Operation == config.PatchTypeRewriteName {
		return RewriteName(obj1, patch, resolver)
	} else if patch.Operation == config.PatchTypeReplace {
		return Replace(obj1, patch)
	} else if patch.Operation == config.PatchTypeRemove {
		return Remove(obj1, patch)
	} else if patch.Operation == config.PatchTypeCopyFromObject {
		return CopyFromObject(obj1, obj2, patch)
	} else if patch.Operation == config.PatchTypeAdd {
		return Add(obj1, patch)
	}

	return fmt.Errorf("patch operation is missing or is not recognized (%s)", patch.Operation)
}

func NewNodeFromString(in string) (*yaml.Node, error) {
	var node yaml.Node
	err := yaml.Unmarshal([]byte(in), &node)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshaling doc: %s\n\n%s", string(in), err)
	}

	return &node, nil
}

func NewNode(raw interface{}) (*yaml.Node, error) {
	doc, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling struct: %+v\n\n%s", raw, err)
	}

	var node yaml.Node
	err = yaml.Unmarshal(doc, &node)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshaling doc: %s\n\n%s", string(doc), err)
	}

	return &node, nil
}

func NewJSONNode(raw interface{}) (*yaml.Node, error) {
	doc, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling struct: %+v\n\n%s", raw, err)
	}

	var node yaml.Node
	err = yaml.Unmarshal(doc, &node)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshaling doc: %s\n\n%s", string(doc), err)
	}

	return &node, nil
}
