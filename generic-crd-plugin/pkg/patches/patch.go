package patches

import (
	"encoding/json"
	"fmt"
	yaml "gopkg.in/yaml.v3"
)

type Patch []Operation

// Apply returns a YAML document that has been mutated per patch
func (p Patch) Apply(doc []byte) ([]byte, error) {
	var node yaml.Node
	err := yaml.Unmarshal(doc, &node)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshaling doc: %s\n\n%s", string(doc), err)
	}

	for _, op := range p {
		err = op.Perform(&node)
		if err != nil {
			return nil, err
		}
	}

	return yaml.Marshal(&node)
}

func NewNode(raw *interface{}) (*yaml.Node, error) {
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
