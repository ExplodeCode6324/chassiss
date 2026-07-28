package contracts

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

const ArchitectureSchema = "chassiss.architecture/v1"

var resourceKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var extensionKeyPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9][a-z0-9-]*/[A-Za-z0-9._-]+$|^[a-z0-9][a-z0-9-]*/[A-Za-z0-9._-]+$`)

func (architecture *Architecture) Validate() error {
	if architecture.Schema != ArchitectureSchema {
		return fmt.Errorf("architecture schema must be %s", ArchitectureSchema)
	}
	if err := protocol.ValidateID(protocol.IDArchitecture, architecture.ID); err != nil {
		return err
	}
	if strings.TrimSpace(architecture.Overview) == "" {
		return fmt.Errorf("architecture overview must be non-empty")
	}
	if len(architecture.Principles) == 0 || hasEmptyString(architecture.Principles) {
		return fmt.Errorf("architecture principles must be a non-empty list of non-empty strings")
	}
	if len(architecture.Modules) == 0 {
		return fmt.Errorf("architecture must contain at least one module")
	}
	if err := validateExtensions(architecture.Extensions); err != nil {
		return err
	}
	resources, err := architecture.resourceMap()
	if err != nil {
		return err
	}
	for id, resource := range resources {
		if strings.TrimSpace(resource.Title) == "" || strings.TrimSpace(resource.Description) == "" {
			return fmt.Errorf("%s title and description must be non-empty", id)
		}
		if strings.HasPrefix(id, "module:") {
			if resource.Owner != "" {
				return fmt.Errorf("%s must not declare owner", id)
			}
			if len(resource.Paths) == 0 {
				return fmt.Errorf("%s paths must be non-empty", id)
			}
		} else if resource.Owner != "" {
			if _, exists := resources[resource.Owner]; !exists || !strings.HasPrefix(resource.Owner, "module:") {
				return fmt.Errorf("%s owner %s is not an existing module", id, resource.Owner)
			}
		}
		if err := validateUniqueStrings(resource.Paths, id+" paths"); err != nil {
			return err
		}
		for _, path := range resource.Paths {
			if _, err := ParsePathScope(path); err != nil {
				return fmt.Errorf("%s path %q: %w", id, path, err)
			}
		}
		if err := validateUniqueStrings(resource.Requires, id+" requires"); err != nil {
			return err
		}
		for _, dependency := range resource.Requires {
			if dependency == id {
				return fmt.Errorf("%s cannot require itself", id)
			}
			if _, exists := resources[dependency]; !exists {
				return fmt.Errorf("%s requires missing resource %s", id, dependency)
			}
		}
	}
	if cycle := graphCycle(resources); len(cycle) > 0 {
		return fmt.Errorf("resource graph contains a cycle: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

func (architecture *Architecture) resourceMap() (map[string]Resource, error) {
	result := make(map[string]Resource)
	views := []struct {
		name      string
		resources map[string]Resource
	}{
		{"module", architecture.Modules},
		{"api", architecture.APIs},
		{"schema", architecture.Schemas},
		{"dependency", architecture.Dependencies},
		{"config", architecture.Configs},
	}
	for _, view := range views {
		for key, resource := range view.resources {
			if !resourceKeyPattern.MatchString(key) {
				return nil, fmt.Errorf("%s resource key %q is invalid", view.name, key)
			}
			id := view.name + ":" + key
			result[id] = resource
		}
	}
	return result, nil
}

func (architecture *Architecture) Resources() map[string]Resource {
	result, _ := architecture.resourceMap()
	return result
}

func (architecture *Architecture) RequiresClosure(start []string) ([]string, error) {
	resources := architecture.Resources()
	visited := make(map[string]bool)
	var walk func(string) error
	walk = func(id string) error {
		if visited[id] {
			return nil
		}
		resource, exists := resources[id]
		if !exists {
			return fmt.Errorf("resource %s does not exist", id)
		}
		visited[id] = true
		for _, dependency := range resource.Requires {
			if err := walk(dependency); err != nil {
				return err
			}
		}
		return nil
	}
	for _, id := range start {
		if err := walk(id); err != nil {
			return nil, err
		}
	}
	result := make([]string, 0, len(visited))
	for id := range visited {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}

func graphCycle(resources map[string]Resource) []string {
	const (
		unseen = iota
		active
		done
	)
	colors := make(map[string]int)
	stack := make([]string, 0)
	var visit func(string) []string
	visit = func(id string) []string {
		switch colors[id] {
		case active:
			for index, entry := range stack {
				if entry == id {
					return append(append([]string(nil), stack[index:]...), id)
				}
			}
		case done:
			return nil
		}
		colors[id] = active
		stack = append(stack, id)
		for _, dependency := range resources[id].Requires {
			if cycle := visit(dependency); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		colors[id] = done
		return nil
	}
	ids := make([]string, 0, len(resources))
	for id := range resources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if cycle := visit(id); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}

func validateExtensions(extensions map[string]any) error {
	for key := range extensions {
		if !extensionKeyPattern.MatchString(key) {
			return fmt.Errorf("extension key %q must use a namespaced identifier", key)
		}
	}
	return nil
}

func validateUniqueStrings(values []string, label string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains duplicate %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func hasEmptyString(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	return false
}
