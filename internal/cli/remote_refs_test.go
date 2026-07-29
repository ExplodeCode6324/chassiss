package cli

import (
	"strings"
	"testing"
)

func TestParseRemoteRef(t *testing.T) {
	oid := strings.Repeat("a", 40)
	actual, exists, err := parseRemoteRef(
		[]byte(oid+"\trefs/heads/main\n"),
		"refs/heads/main",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || actual != oid {
		t.Fatalf("got (%q, %t), want (%q, true)", actual, exists, oid)
	}

	actual, exists, err = parseRemoteRef(nil, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if exists || actual != "" {
		t.Fatalf("missing ref parsed as (%q, %t)", actual, exists)
	}
}

func TestParseRemoteRefRejectsUnexpectedOrInvalidRecords(t *testing.T) {
	cases := [][]byte{
		[]byte(strings.Repeat("a", 40) + "\trefs/heads/other\n"),
		[]byte("not-an-oid\trefs/heads/main\n"),
		[]byte(strings.Repeat("a", 40) + "\trefs/heads/main\n" +
			strings.Repeat("b", 40) + "\trefs/heads/main\n"),
	}
	for _, data := range cases {
		if _, _, err := parseRemoteRef(data, "refs/heads/main"); err == nil {
			t.Fatalf("invalid remote output accepted: %q", data)
		}
	}
}
