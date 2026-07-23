package app

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEventV4AndTrustV1ProtocolGoldenValues(t *testing.T) {
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
	check := CheckSpec{ID: "CHECK-001", Argv: []string{"go", "test", "./..."}, Cwd: ".", Env: map[string]string{"LANG": "C", "VALUE": "<&"}, TimeoutSeconds: 120, VerificationPaths: []string{"verification/**"}}
	submission := Submission{
		ID: "SUB-1", TaskID: "M001-T001", Actor: "agent:developer", BaseCommit: "base", HeadCommit: "head",
		ChangedFiles: []string{"a.go", "dir/b.go"}, Checks: map[string]CheckResult{"CHECK-001": {ID: "CHECK-001", SpecDigest: checkSpecDigest(check), Passed: true, Output: "ok", SnapshotDigest: "tree", VerificationDigest: "verification", CheckedAt: when}},
		Handoff: "ready", Status: "review_pending", CreatedAt: when,
	}
	submissionDigest, err := calculateSubmissionDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical := `{"name":"\u003c\u0026\u2028é","count":42,"at":"2025-02-03T04:05:06.123456789Z","labels":{"a":"first","z":"last"}}`
	if string(canonical) != wantCanonical {
		t.Fatalf("Event V4 canonical JSON changed\ngot:  %s\nwant: %s", canonical, wantCanonical)
	}
	goldenDigests := map[string][2]string{
		"event":      {digestBytes(eventBytes), "sha256:d19efcd69657874d7decb18d906d5f1da19f3af7344e90dc69773b4c4d92c542"},
		"trust":      {digestBytes(trustBytes), "sha256:37aaceff822dbe620a2a45921c9f4960427aa8d8390e0ef27a82320e25570783"},
		"check":      {checkSpecDigest(check), "sha256:63833fe8313130c55f81fa1432d7aecd0fcf70e1f326298bf575b80bd5faa513"},
		"submission": {submissionDigest, "sha256:da9b2479c61f604acde7def8467db05c8fc00dd31ef6a4e78c2db483824b117a"},
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
		reviewRecordedPayload{}, integrationAppliedPayload{}, ownerBaselineAppliedPayload{}, publicationAppliedPayload{},
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
