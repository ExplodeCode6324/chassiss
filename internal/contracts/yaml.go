package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

const (
	maxContractBytes = 4 << 20
	maxYAMLDepth     = 64
	maxYAMLNodes     = 100_000
	maxSafeInteger   = int64(9007199254740991)
)

var decimalInteger = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)

// ParseYAMLSubset parses the YAML 1.2 core subset defined by the v1 contract.
// It rejects duplicate keys and YAML features whose semantics vary across
// implementations before converting the document into JSON-compatible values.
func ParseYAMLSubset(data []byte) (any, error) {
	if len(data) > maxContractBytes {
		return nil, fmt.Errorf("contract exceeds %d bytes", maxContractBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("contract is not valid UTF-8")
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return nil, fmt.Errorf("UTF-8 BOM is prohibited")
	}
	if bytes.Contains(data, []byte{'\r'}) {
		return nil, fmt.Errorf("contract must use LF line endings")
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("multiple YAML documents are prohibited")
	}
	if len(document.Content) != 1 {
		return nil, fmt.Errorf("contract must contain exactly one YAML document")
	}
	count := 0
	return convertYAMLNode(document.Content[0], 0, &count)
}

func convertYAMLNode(node *yaml.Node, depth int, count *int) (any, error) {
	*count++
	if *count > maxYAMLNodes {
		return nil, fmt.Errorf("contract exceeds the node limit")
	}
	if depth > maxYAMLDepth {
		return nil, fmt.Errorf("contract exceeds the nesting limit")
	}
	if node.Anchor != "" || node.Kind == yaml.AliasNode || node.Alias != nil {
		return nil, fmt.Errorf("YAML anchors and aliases are prohibited")
	}
	if strings.HasPrefix(node.Tag, "!") && !strings.HasPrefix(node.Tag, "!!") {
		return nil, fmt.Errorf("custom YAML tag %q is prohibited", node.Tag)
	}
	switch node.Kind {
	case yaml.MappingNode:
		if len(node.Content)%2 != 0 {
			return nil, fmt.Errorf("invalid YAML mapping")
		}
		object := make(map[string]any, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			keyNode := node.Content[index]
			if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
				return nil, fmt.Errorf("mapping keys must be strings")
			}
			key := keyNode.Value
			if key == "<<" {
				return nil, fmt.Errorf("YAML merge keys are prohibited")
			}
			if !norm.NFC.IsNormalString(key) {
				return nil, fmt.Errorf("mapping key %q is not Unicode NFC", key)
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate YAML key %q", key)
			}
			value, err := convertYAMLNode(node.Content[index+1], depth+1, count)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			object[key] = value
		}
		return object, nil
	case yaml.SequenceNode:
		array := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := convertYAMLNode(child, depth+1, count)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		return array, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str":
			if !norm.NFC.IsNormalString(node.Value) {
				return nil, fmt.Errorf("string is not Unicode NFC")
			}
			return node.Value, nil
		case "!!null":
			return nil, nil
		case "!!bool":
			switch node.Value {
			case "true":
				return true, nil
			case "false":
				return false, nil
			default:
				return nil, fmt.Errorf("boolean must be true or false")
			}
		case "!!int":
			if !decimalInteger.MatchString(node.Value) {
				return nil, fmt.Errorf("integer %q is not canonical decimal", node.Value)
			}
			value, err := strconv.ParseInt(node.Value, 10, 64)
			if err != nil || value < -maxSafeInteger || value > maxSafeInteger {
				return nil, fmt.Errorf("integer %q is outside the supported range", node.Value)
			}
			return value, nil
		case "!!float", "!!timestamp":
			return nil, fmt.Errorf("float and timestamp scalars are prohibited")
		default:
			return nil, fmt.Errorf("unsupported YAML scalar tag %q", node.Tag)
		}
	default:
		return nil, fmt.Errorf("unsupported YAML node kind %d", node.Kind)
	}
}

func decodeClosed(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
