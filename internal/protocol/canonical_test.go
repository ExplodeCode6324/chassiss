package protocol

import (
	"strings"
	"testing"
)

func TestCanonicalJSON(t *testing.T) {
	value := map[string]any{
		"z":       int64(1),
		"a":       "<>&",
		"unicode": "雪",
		"control": "\n\t",
	}
	got, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":"<>&","control":"\n\t","unicode":"雪","z":1}`
	if string(got) != want {
		t.Fatalf("canonical JSON mismatch\nwant %s\n got %s", want, got)
	}
}

func TestCanonicalUTF16Ordering(t *testing.T) {
	// RFC 8785 sorts keys by UTF-16 code units. U+10000 begins with D800 and
	// therefore sorts before U+E000.
	got, err := CanonicalJSON(map[string]any{"\ue000": true, "\U00010000": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), `{"𐀀":true`) {
		t.Fatalf("unexpected UTF-16 key order: %s", got)
	}
}

func TestParseRejectsDuplicateAndFloat(t *testing.T) {
	for _, fixture := range []string{
		`{"a":1,"a":2}`,
		`{"a":1.5}`,
		`{"a":9007199254740992}`,
		`{"a":1} trailing`,
	} {
		if _, err := ParseCanonicalInput([]byte(fixture)); err == nil {
			t.Fatalf("expected rejection for %s", fixture)
		}
	}
}

func TestStateBytesHaveSingleLF(t *testing.T) {
	got, err := StateBytes(map[string]any{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{\"a\":\"b\"}\n" {
		t.Fatalf("unexpected state bytes %q", got)
	}
}

func TestCanonicalParserResourceLimits(t *testing.T) {
	deep := strings.Repeat("[", maxCanonicalDepth+2) +
		strings.Repeat("]", maxCanonicalDepth+2)
	if _, err := ParseCanonicalInput([]byte(deep)); err == nil ||
		!strings.Contains(err.Error(), "nesting limit") {
		t.Fatalf("expected nesting-limit rejection, got %v", err)
	}
	oversized := make([]byte, maxCanonicalBytes+1)
	for index := range oversized {
		oversized[index] = ' '
	}
	if _, err := ParseCanonicalInput(oversized); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected byte-limit rejection, got %v", err)
	}
}
