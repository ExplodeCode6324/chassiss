package contracts

import "fmt"

func ParseArchitecture(data []byte) (*Architecture, error) {
	value, err := ParseYAMLSubset(data)
	if err != nil {
		return nil, fmt.Errorf("architecture YAML: %w", err)
	}
	var architecture Architecture
	if err := decodeClosed(value, &architecture); err != nil {
		return nil, fmt.Errorf("architecture schema: %w", err)
	}
	if err := architecture.Validate(); err != nil {
		return nil, err
	}
	return &architecture, nil
}

func ParseTaskbook(data []byte, architecture *Architecture) (*Taskbook, error) {
	value, err := ParseYAMLSubset(data)
	if err != nil {
		return nil, fmt.Errorf("taskbook YAML: %w", err)
	}
	var taskbook Taskbook
	if err := decodeClosed(value, &taskbook); err != nil {
		return nil, fmt.Errorf("taskbook schema: %w", err)
	}
	if err := taskbook.Validate(architecture); err != nil {
		return nil, err
	}
	return &taskbook, nil
}
