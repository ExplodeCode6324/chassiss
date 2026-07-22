package app

import "testing"

func TestAllowedFilePatterns(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{pattern: "go.mod", path: "go.mod", want: true},
		{pattern: "cmd/greet/**", path: "cmd/greet/main.go", want: true},
		{pattern: "cmd/*/main.go", path: "cmd/greet/main.go", want: true},
		{pattern: "cmd/greet/**", path: "docs/requirements.md", want: false},
	}
	for _, test := range tests {
		if got := matchAllowed(test.pattern, test.path); got != test.want {
			t.Errorf("matchAllowed(%q, %q) = %v, want %v", test.pattern, test.path, got, test.want)
		}
	}
}
