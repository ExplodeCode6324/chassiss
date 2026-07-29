package contracts

import (
	"os"
	"path/filepath"
	"testing"
)

func readTemplate(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "templates", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPublishedTemplatesValidate(t *testing.T) {
	architecture, err := ParseArchitecture(readTemplate(t, "architecture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	taskbook, err := ParseTaskbook(readTemplate(t, "taskbook.yaml"), architecture)
	if err != nil {
		t.Fatal(err)
	}
	if architecture.ID != "ARCHITECTURE-001" || taskbook.ID != "TASKBOOK-001" {
		t.Fatal("unexpected template IDs")
	}
}

func TestYAMLSubsetRejectsUnsafeFeatures(t *testing.T) {
	fixtures := map[string]string{
		"duplicate": "a: 1\na: 2\n",
		"float":     "a: 1.5\n",
		"timestamp": "a: 2025-01-01\n",
		"hex":       "a: 0x10\n",
		"anchor":    "a: &x value\nb: *x\n",
		"merge":     "a: {x: 1}\nb: {<<: {x: 2}}\n",
		"tag":       "a: !custom value\n",
		"crlf":      "a: value\r\n",
		"multi-doc": "a: 1\n---\nb: 2\n",
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseYAMLSubset([]byte(fixture)); err == nil {
				t.Fatalf("expected %s rejection", name)
			}
		})
	}
}

func TestPathScopeGrammar(t *testing.T) {
	valid := []string{"src/core/state.go", "src/core/**", "**", "文档/说明.md"}
	for _, value := range valid {
		if _, err := ParsePathScope(value); err != nil {
			t.Errorf("%s: %v", value, err)
		}
	}
	invalid := []string{"/root", ".", "../x", "a//b", "a/*/b", "a/**/b", "a\\b", "e\u0301.txt"}
	for _, value := range invalid {
		if _, err := ParsePathScope(value); err == nil {
			t.Errorf("expected invalid scope %q", value)
		}
	}
}

func TestScopeIntersection(t *testing.T) {
	scope := func(value string) PathScope {
		parsed, err := ParsePathScope(value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	cases := []struct {
		left, right string
		want        bool
	}{
		{"src/**", "src/core/**", true},
		{"src/**", "src/file.go", true},
		{"src/**", "src", false},
		{"src/a.go", "src/b.go", false},
		{"**", "anything/here", true},
	}
	for _, fixture := range cases {
		if got := PathScopesIntersect(scope(fixture.left), scope(fixture.right)); got != fixture.want {
			t.Errorf("%s ∩ %s: got %v want %v", fixture.left, fixture.right, got, fixture.want)
		}
	}
}

func TestTaskConflictReadReadDoesNotConflict(t *testing.T) {
	architecture := &Architecture{
		Schema:     ArchitectureSchema,
		ID:         "ARCHITECTURE-TEST",
		Overview:   "test",
		Principles: []string{"deterministic"},
		Modules: map[string]Resource{
			"a": {Title: "A", Description: "A", Paths: []string{"a/**"}, Requires: []string{"schema:shared"}},
			"b": {Title: "B", Description: "B", Paths: []string{"b/**"}, Requires: []string{"schema:shared"}},
		},
		APIs: map[string]Resource{},
		Schemas: map[string]Resource{
			"shared": {Title: "Shared", Description: "Shared", Paths: []string{}, Requires: []string{}},
		},
		Dependencies: map[string]Resource{},
		Configs:      map[string]Resource{},
	}
	if err := architecture.Validate(); err != nil {
		t.Fatal(err)
	}
	left := Task{Modules: []string{"module:a"}, Writes: []string{"a/**"}}
	right := Task{Modules: []string{"module:b"}, Writes: []string{"b/**"}}
	result, err := Conflict(architecture, "TASK-A", left, "TASK-B", right)
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflict {
		t.Fatalf("read/read overlap unexpectedly conflicted: %v", result.Reasons)
	}
	right.Affects = []string{"schema:shared"}
	result, err = Conflict(architecture, "TASK-A", left, "TASK-B", right)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Conflict {
		t.Fatal("write/read overlap must conflict")
	}
}
