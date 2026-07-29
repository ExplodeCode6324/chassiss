package state

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ExplodeCode6324/chassiss/internal/protocol"
)

func Parse(data []byte, objectFormat string) (*State, error) {
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("State must end with exactly one LF")
	}
	canonicalInput := data[:len(data)-1]
	if len(canonicalInput) > 0 && canonicalInput[len(canonicalInput)-1] == '\n' {
		return nil, fmt.Errorf("State must end with exactly one LF")
	}
	if _, err := protocol.ParseCanonicalInput(canonicalInput); err != nil {
		return nil, err
	}
	var result State
	decoder := json.NewDecoder(bytes.NewReader(canonicalInput))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	expected, err := protocol.StateBytes(result)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(expected, data) {
		return nil, fmt.Errorf("State is not RFC 8785 canonical JSON plus LF")
	}
	if err := result.Validate(objectFormat); err != nil {
		return nil, err
	}
	return &result, nil
}

// ParseReadable is used for published examples. Repository State parsing must
// use Parse, which additionally enforces canonical bytes.
func ParseReadable(data []byte, objectFormat string) (*State, error) {
	if _, err := protocol.ParseCanonicalInput(data); err != nil {
		return nil, err
	}
	var result State
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	if err := result.Validate(objectFormat); err != nil {
		return nil, err
	}
	return &result, nil
}

func Encode(value *State, objectFormat string) ([]byte, error) {
	if value == nil {
		return nil, fmt.Errorf("State is nil")
	}
	if err := value.Validate(objectFormat); err != nil {
		return nil, err
	}
	return protocol.StateBytes(value)
}

func Digest(value *State, objectFormat string) (string, error) {
	data, err := Encode(value, objectFormat)
	if err != nil {
		return "", err
	}
	return protocol.DigestBytes(data), nil
}
