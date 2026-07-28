package contracts

import "testing"

func TestTaskbookDiffRejectsNonReadyChange(t *testing.T) {
	architecture, err := ParseArchitecture(readTemplate(t, "architecture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	oldValue, err := ParseTaskbook(readTemplate(t, "taskbook.yaml"), architecture)
	if err != nil {
		t.Fatal(err)
	}
	newValue := *oldValue
	newValue.Tasks = make(map[string]Task, len(oldValue.Tasks))
	for id, task := range oldValue.Tasks {
		newValue.Tasks[id] = task
	}
	task := newValue.Tasks["TASK-001"]
	task.Goal = "changed meaning"
	newValue.Tasks["TASK-001"] = task
	if _, err := DiffTaskbook(oldValue, &newValue, "old", "new", map[string]string{"TASK-001": "active"}); err == nil {
		t.Fatal("non-ready Task mutation unexpectedly accepted")
	}
	diff, err := DiffTaskbook(oldValue, &newValue, "old", "new", map[string]string{"TASK-001": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.UpdatedReadyTasks) != 1 || diff.UpdatedReadyTasks[0] != "TASK-001" {
		t.Fatalf("unexpected diff %#v", diff)
	}
}
