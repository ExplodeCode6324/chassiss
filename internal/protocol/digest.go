package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func StateBytes(value any) ([]byte, error) {
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return nil, err
	}
	return append(canonical, '\n'), nil
}

func StateDigest(value any) (string, error) {
	data, err := StateBytes(value)
	if err != nil {
		return "", err
	}
	return DigestBytes(data), nil
}

func ObjectDigest(objectType string, value any) (string, error) {
	if !ValidObjectType(objectType) {
		return "", fmt.Errorf("unknown protocol object type %q", objectType)
	}
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	input := make([]byte, 0, 13+len(objectType)+len(canonical))
	input = append(input, []byte("CHASSISS\x00v1\x00")...)
	input = append(input, []byte(objectType)...)
	input = append(input, 0)
	input = append(input, canonical...)
	return DigestBytes(input), nil
}

func ValidateDigest(value string) error {
	if !digestPattern.MatchString(value) {
		return fmt.Errorf("digest must be sha256: followed by 64 lowercase hexadecimal characters")
	}
	return nil
}

func ValidObjectType(value string) bool {
	switch value {
	case "operation", "execution-evidence", "attempt", "submission-evidence",
		"review-context", "review-report", "check-spec", "check-execution-context",
		"check-results", "drift-classification", "changed-paths", "commit-range",
		"taskbook-semantic-diff", "architecture-semantic-diff",
		"taskbook-closure-report", "grant-request-body", "grant-request":
		return true
	default:
		return false
	}
}
