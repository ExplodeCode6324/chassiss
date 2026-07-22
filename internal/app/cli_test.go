package app

import (
	"strings"
	"testing"
)

func TestValidateCommandOptionsRejectsTypos(t *testing.T) {
	parsed := commandArgs{values: map[string]string{"rol": "developer"}, flags: map[string]bool{}}
	if err := validateCommandOptions("next", parsed); err == nil {
		t.Fatal("unknown next option was accepted")
	}
	parsed = commandArgs{values: map[string]string{"role": "developer"}, flags: map[string]bool{}}
	if err := validateCommandOptions("next", parsed); err != nil {
		t.Fatalf("valid next option was rejected: %v", err)
	}
}

func TestProjectBudgetFromArgs(t *testing.T) {
	budget, err := projectBudgetFromArgs(commandArgs{values: map[string]string{}, flags: map[string]bool{}})
	if err != nil || budget != newProjectDefaultTaskBudget {
		t.Fatalf("default budget = %#v, %v", budget, err)
	}
	budget, err = projectBudgetFromArgs(commandArgs{values: map[string]string{
		"max-changed-files": "0", "max-diff-lines": "12", "max-commits": "3",
	}, flags: map[string]bool{}})
	if err != nil || budget != (TaskBudget{MaxChangedFiles: 0, MaxDiffLines: 12, MaxCommits: 3}) {
		t.Fatalf("custom budget = %#v, %v", budget, err)
	}
	if _, err := projectBudgetFromArgs(commandArgs{values: map[string]string{"max-commits": "-1"}, flags: map[string]bool{}}); err == nil {
		t.Fatal("negative Task budget was accepted")
	}
}

func TestTaskTemplateIncludesDefaultBudget(t *testing.T) {
	data, path, err := renderTemplate("", "task", "M001-T001")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, fragment := range []string{"max_changed_files: 100", "max_diff_lines: 20000", "max_commits: 20"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("Task template lacks %q:\n%s", fragment, text)
		}
	}
	if path != "docs/tasks/M001-T001.md" {
		t.Fatalf("Task template path = %q", path)
	}
}
