package app

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEventV2AndTrustV1ProtocolGoldenValues(t *testing.T) {
	when := time.Date(2025, 2, 3, 4, 5, 6, 123456789, time.UTC)
	canonical, err := canonicalJSON(struct {
		Name   string            `json:"name"`
		Count  int64             `json:"count"`
		At     time.Time         `json:"at"`
		Labels map[string]string `json:"labels"`
	}{Name: "<&\u2028é", Count: 42, At: when, Labels: map[string]string{"z": "last", "a": "first"}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := marshalPayload(workCheckpointedPayload{TaskID: "M001-T001", Checkpoint: "<ready>&\u2028"})
	if err != nil {
		t.Fatal(err)
	}
	eventBytes, err := eventSigningBytes(Event{
		Version: EventVersion, ProjectID: "PRJ-1", Sequence: 9, ID: "EVT-9", Type: "work.checkpointed",
		Actor: "agent:developer", Role: "developer", CredentialID: "CRED-1", Resource: "M001-T001",
		OccurredAt: when, PreviousDigest: "sha256:previous", Payload: json.RawMessage(payload), Digest: "ignored", Signature: "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	trustBytes, err := trustSigningBytes(Trust{
		Version: TrustVersion, Revision: 7, ProjectID: "PRJ-1", RootPublicKey: "root-public",
		Grants: []Grant{
			{ID: "CRED-B", Actor: "agent:b", Role: "reviewer", Actions: []string{"review.approve"}, PublicKey: "key-b", IssuedAt: when},
			{ID: "CRED-A", Actor: "agent:a", Role: "developer", Actions: []string{"work.open", "work.submit"}, PublicKey: "key-a", IssuedAt: when},
		},
		Revocations: []Revocation{{CredentialID: "CRED-B", RevokedAt: when.Add(time.Second), Reason: "rotate"}},
		UpdatedAt:   when, Signature: "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	check := CheckSpec{ID: "CHECK-001", Argv: []string{"go", "test", "./..."}, Cwd: ".", Env: map[string]string{"LANG": "C", "VALUE": "<&"}, TimeoutSeconds: 120}
	submission := Submission{
		ID: "SUB-1", TaskID: "M001-T001", Actor: "agent:developer", BaseCommit: "base", HeadCommit: "head",
		ChangedFiles: []string{"a.go", "dir/b.go"}, Checks: map[string]CheckResult{"CHECK-001": {ID: "CHECK-001", SpecDigest: checkSpecDigest(check), Passed: true, Output: "ok", SnapshotDigest: "tree", CheckedAt: when}},
		Handoff: "ready", Status: "review_pending", CreatedAt: when,
	}
	submissionDigest, err := calculateSubmissionDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical := `{"name":"\u003c\u0026\u2028é","count":42,"at":"2025-02-03T04:05:06.123456789Z","labels":{"a":"first","z":"last"}}`
	if string(canonical) != wantCanonical {
		t.Fatalf("Event V2 canonical JSON changed\ngot:  %s\nwant: %s", canonical, wantCanonical)
	}
	goldenDigests := map[string][2]string{
		"event":      {digestBytes(eventBytes), "sha256:8d9a2d1da8e4205b7deeede1d378ad95043e1841cef6366f630dbe9edaf83222"},
		"trust":      {digestBytes(trustBytes), "sha256:37aaceff822dbe620a2a45921c9f4960427aa8d8390e0ef27a82320e25570783"},
		"check":      {checkSpecDigest(check), "sha256:0c57a6fbdb2b2b20dde2ea04f8b67a2d060fa186442f270953fd57f3f94c7ec5"},
		"submission": {submissionDigest, "sha256:4792060e687534172b1805223cff1a3f374eaa3b00435c07155f27e4e189eb3d"},
	}
	for name, pair := range goldenDigests {
		if pair[0] != pair[1] {
			t.Fatalf("%s protocol digest changed: got %s, want %s", name, pair[0], pair[1])
		}
	}
}

func TestSignedProtocolTypesContainNoFloatingPointFields(t *testing.T) {
	for _, value := range []any{
		Event{}, Trust{}, Submission{}, CheckSpec{}, CheckResult{},
		projectInitializedPayload{}, artifactSubmittedPayload{}, artifactAcceptedPayload{}, artifactRejectedPayload{},
		missionPayload{}, missionBlockedPayload{}, missionAcceptancePayload{},
		taskClaimedPayload{}, taskBlockedPayload{}, taskPayload{}, taskCancelledPayload{}, taskSupersededPayload{},
		workOpenedPayload{}, workCheckedPayload{}, workCheckpointedPayload{}, workSubmittedPayload{},
		reviewRecordedPayload{}, integrationAppliedPayload{}, publicationAppliedPayload{},
	} {
		assertNoFloatType(t, reflect.TypeOf(value), map[reflect.Type]bool{})
	}
}

func assertNoFloatType(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	if typ == nil || seen[typ] || typ == reflect.TypeOf(time.Time{}) || typ == reflect.TypeOf(json.RawMessage{}) {
		return
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Float32, reflect.Float64:
		t.Fatalf("signed protocol contains floating-point type %s", typ)
	case reflect.Pointer, reflect.Slice, reflect.Array:
		assertNoFloatType(t, typ.Elem(), seen)
	case reflect.Map:
		assertNoFloatType(t, typ.Key(), seen)
		assertNoFloatType(t, typ.Elem(), seen)
	case reflect.Struct:
		for index := 0; index < typ.NumField(); index++ {
			assertNoFloatType(t, typ.Field(index).Type, seen)
		}
	}
}
