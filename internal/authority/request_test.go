package authority

import (
	"testing"

	"github.com/ExplodeCode6324/chassiss/internal/cryptoutil"
	"github.com/ExplodeCode6324/chassiss/internal/state"
)

func TestGrantRequestProof(t *testing.T) {
	key, err := cryptoutil.GenerateFileKey(t.TempDir(), "KEY-AGENT-01")
	if err != nil {
		t.Fatal(err)
	}
	maxActive := int64(1)
	request, err := NewRequest(
		"PRJ-TEST", "agent-one", "KEY-AGENT-01", key.PublicKey, key.Handle,
		DeveloperCapabilities(),
		state.Scope{Tasks: []string{"TASK-*"}, Resources: []string{"module:*"}},
		state.Limits{Mode: "bounded", MaxActiveTasks: &maxActive},
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeRequest(*request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Actor != "agent-one" {
		t.Fatalf("unexpected actor %s", decoded.Actor)
	}
	decoded.Actor = "agent-two"
	if err := decoded.Validate(); err == nil {
		t.Fatal("tampered request proof unexpectedly verified")
	}
}
