package protocol

import "testing"

func TestIdentifiers(t *testing.T) {
	valid := []struct {
		kind  IDKind
		value string
	}{
		{IDProject, "PRJ-EXAMPLE"},
		{IDTask, "TASK-001-A"},
		{IDKey, "KEY-ROOT-01"},
	}
	for _, fixture := range valid {
		if err := ValidateID(fixture.kind, fixture.value); err != nil {
			t.Errorf("%s: %v", fixture.value, err)
		}
	}
	for _, value := range []string{"PRJ-lower", "TASK--A", "KEY-A_1"} {
		if err := ValidateID(IDKind(stringsBeforeHyphen(value)), value); err == nil {
			t.Errorf("expected invalid ID %s", value)
		}
	}
}

func stringsBeforeHyphen(value string) string {
	for index, r := range value {
		if r == '-' {
			return value[:index]
		}
	}
	return value
}

func TestOperationIDGeneration(t *testing.T) {
	first, err := NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOperationID()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateOperationID(first); err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("random operation IDs unexpectedly collided")
	}
}
