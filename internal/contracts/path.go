package contracts

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var protectedExact = map[string]struct{}{
	".chassiss/state.json":   {},
	"docs/architecture.yaml": {},
	"docs/taskbook.yaml":     {},
}

type PathScope struct {
	Raw       string
	All       bool
	Prefix    string
	ExactPath string
}

func ParsePathScope(value string) (PathScope, error) {
	if value == "**" {
		return PathScope{Raw: value, All: true}, nil
	}
	if err := ValidateRepoPath(strings.TrimSuffix(value, "/**")); err != nil {
		return PathScope{}, err
	}
	if strings.HasSuffix(value, "/**") {
		prefix := strings.TrimSuffix(value, "/**")
		if strings.Contains(prefix, "*") {
			return PathScope{}, fmt.Errorf("wildcards are only allowed as a terminal /**")
		}
		return PathScope{Raw: value, Prefix: prefix}, nil
	}
	if strings.Contains(value, "*") {
		return PathScope{}, fmt.Errorf("wildcards are only allowed as ** or terminal /**")
	}
	return PathScope{Raw: value, ExactPath: value}, nil
}

func ValidateRepoPath(value string) error {
	if value == "" || value == "." {
		return fmt.Errorf("path must be a non-empty repository-relative path")
	}
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) {
		return fmt.Errorf("path must be valid UTF-8 in Unicode NFC")
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.Contains(value, "//") || strings.Contains(value, "\\") {
		return fmt.Errorf("path must use normalized repository-relative / separators")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("path contains a prohibited segment")
		}
		for _, r := range segment {
			if r == 0 || (r <= 0x7f && unicode.IsControl(r)) {
				return fmt.Errorf("path contains a prohibited control character")
			}
		}
	}
	return nil
}

func (scope PathScope) Matches(path string) bool {
	if scope.All {
		return true
	}
	if scope.ExactPath != "" {
		return path == scope.ExactPath
	}
	return strings.HasPrefix(path, scope.Prefix+"/") && len(path) > len(scope.Prefix)+1
}

func PathScopesIntersect(left, right PathScope) bool {
	if left.All || right.All {
		return true
	}
	if left.ExactPath != "" {
		return right.Matches(left.ExactPath)
	}
	if right.ExactPath != "" {
		return left.Matches(right.ExactPath)
	}
	return left.Prefix == right.Prefix ||
		strings.HasPrefix(left.Prefix, right.Prefix+"/") ||
		strings.HasPrefix(right.Prefix, left.Prefix+"/")
}

func PathWithinAny(path string, scopes []PathScope) bool {
	for _, scope := range scopes {
		if scope.Matches(path) {
			return true
		}
	}
	return false
}

func IsProtectedPath(path string) bool {
	if _, exists := protectedExact[path]; exists {
		return true
	}
	return strings.HasPrefix(path, "docs/taskbooks/archive/")
}
