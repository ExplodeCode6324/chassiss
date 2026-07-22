package app

import "testing"

func TestValidateCommandOptionsRejectsTypos(t *testing.T) {
	parsed := commandArgs{values: map[string]string{"rol": "developer"}, flags: map[string]bool{}}
	if err := validateCommandOptions("next", parsed); err == nil {
		t.Fatal("unknown next option was accepted")
	}
	parsed = commandArgs{values: map[string]string{"role": "developer"}, flags: map[string]bool{}}
	if err := validateCommandOptions("next", parsed); err != nil {
		t.Fatalf("valid next option was rejected: %v", err)
	}
}
