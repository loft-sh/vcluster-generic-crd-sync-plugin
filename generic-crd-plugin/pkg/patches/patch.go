package patches

import (
	"encoding/json"
	"fmt"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v3"
)

type NameResolver interface {
	TranslateName(name string) (string, error)
}

func ApplyPatches(obj1, obj2 *yaml.Node, patches []*config.Patch, nameResolver NameResolver) error {
	for _, p := range patches {
		err := ApplyPatch(obj1, obj2, p, nameResolver)
		if err != nil {
			return errors.Wrap(err, "apply patch")
		}
	}

	return nil
}

func ApplyPatch(obj1, obj2 *yaml.Node, patch *config.Patch, resolver NameResolver) error {
	if patch.Type == config.PatchTypeRewriteName {
		return RewriteName(obj1, patch, resolver)
	} else if patch.Type == config.PatchTypeRewriteNamespace {
		return RewriteNamespace(obj1, patch, resolver)
	} else if patch.Type == config.PatchTypeReplace {
		return Replace(obj1, patch)
	} else if patch.Type == config.PatchTypeRemove {
		return Remove(obj1, patch)
	} else if patch.Type == config.PatchTypeCopyFromObject {
		return CopyFromObject(obj1, obj2, patch)
	} else if patch.Type == config.PatchTypeAdd {
		return Add(obj1, patch)
	}

	return fmt.Errorf("patch type is missing or is not recognized (%s)", patch.Type)
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
