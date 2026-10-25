package configstore

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"strconv"
)

func decodeStrictYAMLNode(value *yaml.Node, target any, allowed ...string) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("connection payload must be a mapping")
	}
	known := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		known[field] = struct{}{}
	}
	seen := make(map[string]struct{}, len(allowed))
	for index := 0; index < len(value.Content); index += 2 {
		key := value.Content[index]
		if key.Kind != yaml.ScalarNode {
			return fmt.Errorf("connection field name must be a string")
		}
		if _, exists := known[key.Value]; !exists {
			return fmt.Errorf("field %q is not recognized", key.Value)
		}
		if _, duplicate := seen[key.Value]; duplicate {
			return fmt.Errorf("field %q is duplicated", key.Value)
		}
		seen[key.Value] = struct{}{}
	}
	return value.Decode(target)
}

// Current Targets have a closed reference grammar. In particular YAML coercion
// must not turn a quoted, hexadecimal or omitted version into durable authority.
func (dto *targetDTO) UnmarshalYAML(node *yaml.Node) error {
	type plainTarget targetDTO
	var value plainTarget
	if err := decodeStrictYAMLNode(node, &value, "id", "version", "model", "protocol", "connection_id"); err != nil {
		return err
	}
	if len(node.Content) != 10 {
		return fmt.Errorf("reference target requires exactly five fields")
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, field := node.Content[i].Value, node.Content[i+1]
		if field.Kind != yaml.ScalarNode {
			return fmt.Errorf("reference target fields must be scalar")
		}
		if key == "version" {
			if field.Tag != "!!int" || value.Version == nil || *value.Version == 0 || strconv.FormatUint(*value.Version, 10) != field.Value {
				return fmt.Errorf("positive canonical target version required")
			}
		} else if field.Tag != "!!str" || field.Value == "" {
			return fmt.Errorf("nonempty target string required")
		}
	}
	*dto = targetDTO(value)
	return nil
}
