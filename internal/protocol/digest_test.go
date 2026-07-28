package protocol

import "testing"

func TestDomainSeparatedDigests(t *testing.T) {
	value := map[string]any{"x": "same"}
	operation, err := ObjectDigest("operation", value)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ObjectDigest("attempt", value)
	if err != nil {
		t.Fatal(err)
	}
	if operation == attempt {
		t.Fatal("different object domains must not share a digest")
	}
	if err := ValidateDigest(operation); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownDigestDomainRejected(t *testing.T) {
	if _, err := ObjectDigest("invented", map[string]any{}); err == nil {
		t.Fatal("expected unknown domain rejection")
	}
}
