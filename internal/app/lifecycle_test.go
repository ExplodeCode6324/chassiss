package app

import (
	"reflect"
	"testing"
	"time"
)

func TestCalculateSubmissionDigestBindsImmutableManifest(t *testing.T) {
	submission := Submission{
		ID: "SUB-1", TaskID: "M001-T001", Actor: "agent:developer",
		BaseCommit: "base", HeadCommit: "head", ChangedFiles: []string{"code.go"},
		Checks:  map[string]CheckResult{"CHECK-001": {ID: "CHECK-001", Passed: true, SnapshotDigest: "snapshot"}},
		Handoff: "ready", Status: "review_pending", CreatedAt: time.Unix(1, 0).UTC(),
	}
	first, err := calculateSubmissionDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	submission.Status = "approved"
	submission.ReviewID = "REV-1"
	second, err := calculateSubmissionDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("mutable review state must not change the submission manifest digest")
	}
	submission.Handoff = "different"
	third, err := calculateSubmissionDigest(submission)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("handoff changes must invalidate the submission digest")
	}
}

func TestEffectiveExpected(t *testing.T) {
	state := State{Revision: 7}
	if got, err := effectiveExpected(state, -1); err != nil || got != 7 {
		t.Fatalf("default expected = %d, %v; want 7, nil", got, err)
	}
	if _, err := effectiveExpected(state, 6); err == nil {
		t.Fatal("stale explicit revision was accepted")
	}
}

func TestValidateTaskChecksBindsSnapshot(t *testing.T) {
	task := TaskState{
		Checks: []CheckSpec{{ID: "CHECK-001", Argv: []string{"go", "test", "./..."}, Cwd: ".", Env: map[string]string{}, TimeoutSeconds: 120}},
	}
	task.CheckResults = map[string]CheckResult{"CHECK-001": {ID: "CHECK-001", SpecDigest: checkSpecDigest(task.Checks[0]), Passed: true, SnapshotDigest: "snapshot-a"}}
	if err := validateTaskChecks(task, "snapshot-a"); err != nil {
		t.Fatalf("current check was rejected: %v", err)
	}
	if err := validateTaskChecks(task, "snapshot-b"); err == nil {
		t.Fatal("stale check snapshot was accepted")
	}
	forged := task
	result := forged.CheckResults["CHECK-001"]
	result.SpecDigest = "sha256:forged"
	forged.CheckResults = map[string]CheckResult{"CHECK-001": result}
	if err := validateTaskChecks(forged, "snapshot-a"); err == nil {
		t.Fatal("check result from a different Task contract was accepted")
	}
}

func TestNextActionsRequireChecksAndOfferMissionAcceptance(t *testing.T) {
	state := State{
		ActiveMission: "M001",
		Missions:      map[string]MissionState{"M001": {ID: "M001", Status: "active", TaskIDs: []string{"M001-T001"}}},
		Tasks: map[string]TaskState{"M001-T001": {
			ID: "M001-T001", MissionID: "M001", Status: "integrated", Owner: "agent:developer",
		}},
	}
	want := []string{"mission.submit-acceptance M001"}
	if got := nextActions(state, "orchestrator", "agent:orchestrator"); !reflect.DeepEqual(got, want) {
		t.Fatalf("orchestrator actions = %#v, want %#v", got, want)
	}
	state.Tasks["M001-T001"] = TaskState{
		ID: "M001-T001", MissionID: "M001", Status: "in_progress", Owner: "agent:developer",
		Checks: []CheckSpec{{ID: "CHECK-001", Argv: []string{"true"}, Cwd: ".", Env: map[string]string{}, TimeoutSeconds: 10}}, CheckResults: map[string]CheckResult{},
	}
	want = []string{"work.check M001-T001"}
	if got := nextActions(state, "developer", "agent:developer"); !reflect.DeepEqual(got, want) {
		t.Fatalf("developer actions = %#v, want %#v", got, want)
	}
}

func TestMissionBlockDisablesDeveloperAndReviewerProgress(t *testing.T) {
	state := State{
		ActiveMission: "M001",
		Missions:      map[string]MissionState{"M001": {ID: "M001", Status: "blocked", TaskIDs: []string{"M001-T001"}, PreviousStatus: "active", BlockReason: "stop"}},
		Tasks: map[string]TaskState{"M001-T001": {
			ID: "M001-T001", MissionID: "M001", Status: "in_progress", Owner: "agent:developer",
		}},
		Submissions: map[string]Submission{"SUB-1": {ID: "SUB-1", TaskID: "M001-T001", Actor: "agent:developer", Status: "review_pending"}},
	}
	if err := requireMissionExecutable(state, "M001"); err == nil {
		t.Fatal("blocked mission was treated as executable")
	} else if typed, ok := err.(*CLIError); !ok || typed.Code != "CHS-MISSION-BLOCKED" {
		t.Fatalf("error = %#v, want CHS-MISSION-BLOCKED", err)
	}
	if got := nextActions(state, "developer", "agent:developer"); len(got) != 0 {
		t.Fatalf("blocked developer actions = %#v, want none", got)
	}
	if got := nextActions(state, "reviewer", "agent:reviewer"); len(got) != 0 {
		t.Fatalf("blocked reviewer actions = %#v, want none", got)
	}
	want := []string{"mission.resume M001"}
	if got := nextActions(state, "orchestrator", "agent:orchestrator"); !reflect.DeepEqual(got, want) {
		t.Fatalf("blocked orchestrator actions = %#v, want %#v", got, want)
	}
}
