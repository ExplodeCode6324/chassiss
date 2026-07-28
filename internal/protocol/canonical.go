package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	maxSafeInteger    int64 = 9007199254740991
	maxCanonicalBytes       = 4 << 20
	maxCanonicalDepth       = 64
	maxCanonicalNodes       = 100_000
)

// ParseCanonicalInput decodes a JSON value while detecting duplicate keys,
// rejecting non-integer numbers, and keeping integer text exact.
func ParseCanonicalInput(data []byte) (any, error) {
	if len(data) > maxCanonicalBytes {
		return nil, fmt.Errorf("JSON exceeds %d bytes", maxCanonicalBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("JSON is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	nodes := 0
	value, err := decodeJSONValue(dec, 0, &nodes)
	if err != nil {
		return nil, err
	}
	if token, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing JSON token %v", token)
		}
		return nil, fmt.Errorf("unexpected trailing JSON: %w", err)
	}
	return value, nil
}

func decodeJSONValue(dec *json.Decoder, depth int, nodes *int) (any, error) {
	*nodes++
	if *nodes > maxCanonicalNodes {
		return nil, fmt.Errorf("JSON exceeds the node limit")
	}
	if depth > maxCanonicalDepth {
		return nil, fmt.Errorf("JSON exceeds the nesting limit")
	}
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := make(map[string]any)
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string")
				}
				if _, exists := object[key]; exists {
					return nil, fmt.Errorf("duplicate JSON key %q", key)
				}
				child, err := decodeJSONValue(dec, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				object[key] = child
			}
			if end, err := dec.Token(); err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("unterminated JSON object")
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for dec.More() {
				child, err := decodeJSONValue(dec, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			if end, err := dec.Token(); err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("unterminated JSON array")
			}
			return array, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", value)
		}
	case json.Number:
		text := value.String()
		if strings.ContainsAny(text, ".eE") {
			return nil, fmt.Errorf("floating-point JSON number %q is prohibited", text)
		}
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil || number < -maxSafeInteger || number > maxSafeInteger {
			return nil, fmt.Errorf("integer %q is outside the supported range", text)
		}
		return number, nil
	case string, bool, nil:
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported JSON token %T", value)
	}
}

// CanonicalJSON produces RFC 8785-compatible bytes for the protocol's
// integer-only data model.
func CanonicalJSON(value any) ([]byte, error) {
	var normalized any
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	normalized, err = ParseCanonicalInput(raw)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, normalized); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeCanonical(out *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if typed {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		return writeJSONString(out, typed)
	case int64:
		out.WriteString(strconv.FormatInt(typed, 10))
	case []any:
		out.WriteByte('[')
		for index, child := range typed {
			if index > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, child); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })
		out.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				out.WriteByte(',')
			}
			if err := writeJSONString(out, key); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := writeCanonical(out, typed[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON type %T", value)
	}
	return nil
}

func lessUTF16(left, right string) bool {
	a := utf16.Encode([]rune(left))
	b := utf16.Encode([]rune(right))
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func writeJSONString(out *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("string is not valid UTF-8")
	}
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return nil
}
