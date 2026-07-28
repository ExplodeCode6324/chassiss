package protocol

import (
	"crypto/rand"
	"fmt"
	"regexp"
)

const (
	ProtocolID      = "chassiss/v1"
	StateSchema     = "chassiss.state/v1"
	OperationSchema = "chassiss.operation/v1"
	EvidenceSchema  = "chassiss.execution-evidence/v1"
	CLISchema       = "chassiss.cli/v1"
	ContextSchema   = "chassiss.context/v1"
)

type IDKind string

const (
	IDProject      IDKind = "PRJ"
	IDTaskbook     IDKind = "TASKBOOK"
	IDArchitecture IDKind = "ARCHITECTURE"
	IDTask         IDKind = "TASK"
	IDRequirement  IDKind = "REQ"
	IDConstraint   IDKind = "CON"
	IDCheck        IDKind = "CHECK"
	IDGrant        IDKind = "GRT"
	IDKey          IDKind = "KEY"
)

var (
	stableTokenPattern = regexp.MustCompile(`^[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	actorPattern       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	operationPattern   = regexp.MustCompile(`^OPR-[0-9A-HJKMNP-TV-Z]{26}$`)
)

func ValidateID(kind IDKind, value string) error {
	prefix := string(kind) + "-"
	if len(value) <= len(prefix) || len(value) > len(prefix)+64 || value[:len(prefix)] != prefix {
		return fmt.Errorf("%s must use %s- followed by a token of at most 64 characters", value, kind)
	}
	if !stableTokenPattern.MatchString(value[len(prefix):]) {
		return fmt.Errorf("%s has an invalid stable token", value)
	}
	return nil
}

func ValidateActor(value string) error {
	if len(value) == 0 || len(value) > 64 || !actorPattern.MatchString(value) {
		return fmt.Errorf("actor must contain lowercase ASCII letters, digits, and single hyphens")
	}
	return nil
}

func ValidateOperationID(value string) error {
	if !operationPattern.MatchString(value) {
		return fmt.Errorf("operation ID must be OPR- plus 26 Crockford Base32 characters")
	}
	return nil
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewOperationID creates 130 random bits and renders exactly 26 Crockford
// Base32 characters. It does not depend on wall-clock uniqueness.
func NewOperationID() (string, error) {
	var raw [17]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	out := make([]byte, 26)
	var buffer uint32
	var bits uint
	source := raw[:]
	for i := range out {
		for bits < 5 {
			buffer = (buffer << 8) | uint32(source[0])
			source = source[1:]
			bits += 8
		}
		shift := bits - 5
		out[i] = crockford[(buffer>>shift)&31]
		if shift == 0 {
			buffer = 0
		} else {
			buffer &= (1 << shift) - 1
		}
		bits = shift
	}
	return "OPR-" + string(out), nil
}
