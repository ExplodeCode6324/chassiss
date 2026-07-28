package contracts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

const TaskbookSchema = "chassiss.taskbook/v1"
const maxCoreLimit int64 = 2147483647

func (taskbook *Taskbook) Validate(architecture *Architecture) error {
	if architecture == nil {
		return fmt.Errorf("architecture is required to validate a taskbook")
	}
	if taskbook.Schema != TaskbookSchema {
		return fmt.Errorf("taskbook schema must be %s", TaskbookSchema)
	}
	if err := protocol.ValidateID(protocol.IDTaskbook, taskbook.ID); err != nil {
		return err
	}
	if err := validateExtensions(taskbook.Extensions); err != nil {
		return err
	}
	if strings.TrimSpace(taskbook.Workflow.Title) == "" ||
		strings.TrimSpace(taskbook.Workflow.Outcome) == "" ||
		len(taskbook.Workflow.CompletionCriteria) == 0 ||
		hasEmptyString(taskbook.Workflow.CompletionCriteria) {
		return fmt.Errorf("workflow title, outcome, and completion criteria must be non-empty")
	}
	if len(taskbook.Workflow.Checks) == 0 {
		return fmt.Errorf("workflow checks must contain at least one CheckSpec")
	}
	checkIDs := make(map[string]string)
	for index, check := range taskbook.Workflow.Checks {
		if err := check.Validate(); err != nil {
			return fmt.Errorf("workflow check %d: %w", index, err)
		}
		if previous, exists := checkIDs[check.ID]; exists {
			return fmt.Errorf("check ID %s is shared by %s and workflow", check.ID, previous)
		}
		checkIDs[check.ID] = "workflow"
	}
	for id, requirement := range taskbook.Requirements {
		if err := protocol.ValidateID(protocol.IDRequirement, id); err != nil {
			return err
		}
		if strings.TrimSpace(requirement.Title) == "" || strings.TrimSpace(requirement.Statement) == "" ||
			hasEmptyString(requirement.Acceptance) {
			return fmt.Errorf("%s has an empty required field", id)
		}
	}
	for id, constraint := range taskbook.Constraints {
		if err := protocol.ValidateID(protocol.IDConstraint, id); err != nil {
			return err
		}
		if strings.TrimSpace(constraint.Title) == "" || strings.TrimSpace(constraint.Rule) == "" {
			return fmt.Errorf("%s has an empty required field", id)
		}
	}
	resources := architecture.Resources()
	for id, task := range taskbook.Tasks {
		if err := protocol.ValidateID(protocol.IDTask, id); err != nil {
			return err
		}
		if err := task.validate(id, taskbook, architecture, resources, checkIDs); err != nil {
			return err
		}
	}
	if cycle := taskDependencyCycle(taskbook.Tasks); len(cycle) > 0 {
		return fmt.Errorf("task dependency graph contains a cycle: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

func (task Task) validate(id string, taskbook *Taskbook, architecture *Architecture, resources map[string]Resource, checkIDs map[string]string) error {
	if strings.TrimSpace(task.Title) == "" || strings.TrimSpace(task.Goal) == "" {
		return fmt.Errorf("%s title and goal must be non-empty", id)
	}
	if len(task.Modules) == 0 {
		return fmt.Errorf("%s modules must be non-empty", id)
	}
	setArrays := []struct {
		label  string
		values []string
	}{
		{"requirements", task.Requirements},
		{"constraints", task.Constraints},
		{"modules", task.Modules},
		{"depends_on", task.DependsOn},
		{"writes", task.Writes},
		{"affects", task.Affects},
		{"supersedes", task.Supersedes},
	}
	for _, field := range setArrays {
		if err := validateUniqueStrings(field.values, id+" "+field.label); err != nil {
			return err
		}
	}
	if hasEmptyString(task.Deliverables) || hasEmptyString(task.OutOfScope) ||
		hasEmptyString(task.StopConditions) || hasEmptyString(task.ReviewerAttention) {
		return fmt.Errorf("%s ordered text lists cannot contain empty strings", id)
	}
	for _, reference := range task.Requirements {
		if _, exists := taskbook.Requirements[reference]; !exists {
			return fmt.Errorf("%s references missing requirement %s", id, reference)
		}
	}
	for _, reference := range task.Constraints {
		if _, exists := taskbook.Constraints[reference]; !exists {
			return fmt.Errorf("%s references missing constraint %s", id, reference)
		}
	}
	for _, dependency := range task.DependsOn {
		if dependency == id {
			return fmt.Errorf("%s cannot depend on itself", id)
		}
		if _, exists := taskbook.Tasks[dependency]; !exists {
			return fmt.Errorf("%s depends on missing task %s", id, dependency)
		}
	}
	for _, superseded := range task.Supersedes {
		if superseded == id {
			return fmt.Errorf("%s cannot supersede itself", id)
		}
		if _, exists := taskbook.Tasks[superseded]; !exists {
			return fmt.Errorf("%s supersedes missing task %s", id, superseded)
		}
	}
	for _, module := range task.Modules {
		if !strings.HasPrefix(module, "module:") {
			return fmt.Errorf("%s module reference %s is not typed module:<id>", id, module)
		}
		if _, exists := resources[module]; !exists {
			return fmt.Errorf("%s references missing module %s", id, module)
		}
	}
	for _, affected := range task.Affects {
		if _, exists := resources[affected]; !exists {
			return fmt.Errorf("%s affects missing resource %s", id, affected)
		}
	}
	writeScopes := make([]PathScope, 0, len(task.Writes))
	for _, path := range task.Writes {
		scope, err := ParsePathScope(path)
		if err != nil {
			return fmt.Errorf("%s write scope %q: %w", id, path, err)
		}
		writeScopes = append(writeScopes, scope)
	}
	moduleScopes := make([]PathScope, 0)
	for _, module := range task.Modules {
		for _, path := range resources[module].Paths {
			scope, _ := ParsePathScope(path)
			moduleScopes = append(moduleScopes, scope)
		}
	}
	for _, write := range writeScopes {
		intersects := false
		for _, module := range moduleScopes {
			if PathScopesIntersect(write, module) {
				intersects = true
				break
			}
		}
		if !intersects {
			return fmt.Errorf("%s write scope %s does not intersect a selected module path", id, write.Raw)
		}
	}
	for index, check := range task.Checks {
		if err := check.Validate(); err != nil {
			return fmt.Errorf("%s check %d: %w", id, index, err)
		}
		if previous, exists := checkIDs[check.ID]; exists {
			return fmt.Errorf("check ID %s is shared by %s and %s", check.ID, previous, id)
		}
		checkIDs[check.ID] = id
	}
	if task.ChangeLimits != nil &&
		(task.ChangeLimits.MaxChangedPaths < 1 || task.ChangeLimits.MaxChangedPaths > maxCoreLimit) {
		return fmt.Errorf("%s max_changed_paths must be in [1,%d]", id, maxCoreLimit)
	}
	return nil
}

func (check CheckSpec) Validate() error {
	if err := protocol.ValidateID(protocol.IDCheck, check.ID); err != nil {
		return err
	}
	if len(check.Argv) == 0 || hasEmptyString(check.Argv) {
		return fmt.Errorf("CheckSpec argv must be a non-empty string array")
	}
	if check.Cwd != "." {
		if strings.Contains(check.Cwd, "*") {
			return fmt.Errorf("CheckSpec cwd cannot contain wildcard")
		}
		if err := ValidateRepoPath(check.Cwd); err != nil {
			return fmt.Errorf("CheckSpec cwd: %w", err)
		}
	}
	if check.TimeoutSeconds < 1 || check.TimeoutSeconds > maxCoreLimit {
		return fmt.Errorf("CheckSpec timeout_seconds must be in [1,%d]", maxCoreLimit)
	}
	return nil
}

func taskDependencyCycle(tasks map[string]Task) []string {
	colors := make(map[string]int)
	stack := make([]string, 0)
	var visit func(string) []string
	visit = func(id string) []string {
		if colors[id] == 1 {
			for index, entry := range stack {
				if entry == id {
					return append(append([]string(nil), stack[index:]...), id)
				}
			}
		}
		if colors[id] == 2 {
			return nil
		}
		colors[id] = 1
		stack = append(stack, id)
		for _, dependency := range tasks[id].DependsOn {
			if cycle := visit(dependency); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		colors[id] = 2
		return nil
	}
	ids := make([]string, 0, len(tasks))
	for id := range tasks {
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

type TaskConflict struct {
	Conflict bool
	Reasons  []string
}

func Conflict(architecture *Architecture, leftID string, left Task, rightID string, right Task) (TaskConflict, error) {
	leftWrites, err := parseScopes(left.Writes)
	if err != nil {
		return TaskConflict{}, err
	}
	rightWrites, err := parseScopes(right.Writes)
	if err != nil {
		return TaskConflict{}, err
	}
	reasons := make([]string, 0)
	for _, a := range leftWrites {
		for _, b := range rightWrites {
			if PathScopesIntersect(a, b) {
				reasons = append(reasons, fmt.Sprintf("path:%s∩%s", a.Raw, b.Raw))
			}
		}
	}
	leftRead, err := taskReadSet(architecture, left)
	if err != nil {
		return TaskConflict{}, err
	}
	rightRead, err := taskReadSet(architecture, right)
	if err != nil {
		return TaskConflict{}, err
	}
	leftWrite := stringSet(left.Affects)
	rightWrite := stringSet(right.Affects)
	appendIntersections := func(kind string, a, b map[string]struct{}) {
		for value := range a {
			if _, exists := b[value]; exists {
				reasons = append(reasons, kind+":"+value)
			}
		}
	}
	appendIntersections("write-write", leftWrite, rightWrite)
	appendIntersections("write-read", leftWrite, rightRead)
	appendIntersections("read-write", leftRead, rightWrite)
	sort.Strings(reasons)
	return TaskConflict{Conflict: len(reasons) > 0, Reasons: reasons}, nil
}

func taskReadSet(architecture *Architecture, task Task) (map[string]struct{}, error) {
	start := append(append([]string(nil), task.Modules...), task.Affects...)
	closure, err := architecture.RequiresClosure(start)
	if err != nil {
		return nil, err
	}
	writes := stringSet(task.Affects)
	result := make(map[string]struct{})
	for _, resource := range closure {
		if _, written := writes[resource]; !written {
			result[resource] = struct{}{}
		}
	}
	return result, nil
}

func parseScopes(values []string) ([]PathScope, error) {
	result := make([]PathScope, 0, len(values))
	for _, value := range values {
		scope, err := ParsePathScope(value)
		if err != nil {
			return nil, err
		}
		result = append(result, scope)
	}
	return result, nil
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
