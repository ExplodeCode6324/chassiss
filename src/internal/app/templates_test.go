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

func TestIndependentVerificationCannotOverlapDeveloperScope(t *testing.T) {
	check := CheckSpec{
		ID: "CHECK-001", Argv: []string{"go", "test", "./..."}, Cwd: ".", Env: map[string]string{}, TimeoutSeconds: 10,
		VerificationPaths: []string{"tests/acceptance/**"},
	}
	if err := validateIndependentVerification([]string{"src/**"}, check); err != nil {
		t.Fatalf("independent verification was rejected: %v", err)
	}
	if err := validateIndependentVerification([]string{"src/**", "tests/**"}, check); err == nil {
		t.Fatal("verification paths overlapping Developer scope were accepted")
	}
	check.VerificationPaths = nil
	if err := validateIndependentVerification([]string{"src/**"}, check); err == nil {
		t.Fatal("acceptance check without independent verification paths was accepted")
	}
	check.VerificationPaths = []string{"tests/../src/**"}
	if err := validateIndependentVerification([]string{"lib/**"}, check); err == nil {
		t.Fatal("non-canonical verification path was accepted")
	}
}
