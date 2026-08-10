package cli

import "strings"

// symlinkTargetEscapesRepository applies both POSIX and Windows path semantics
// regardless of the host performing validation. A source snapshot admitted on
// one platform must remain repository-contained when materialized on another.
func symlinkTargetEscapesRepository(target string) bool {
	portable := strings.ReplaceAll(target, `\`, "/")
	if strings.HasPrefix(portable, "/") || hasWindowsDrivePrefix(portable) {
		return true
	}
	return containsParentSegment(portable)
}

func hasWindowsDrivePrefix(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	letter := value[0]
	return (letter >= 'A' && letter <= 'Z') || (letter >= 'a' && letter <= 'z')
}

func containsParentSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}
