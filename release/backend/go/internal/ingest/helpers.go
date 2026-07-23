// Package ingest: helpers.go — вспомогательные функции для ингестии.
package ingest

import (
	"bytes"
	"encoding/json"
)

// bytesReader создаёт *bytes.Reader из []byte (для zip.NewReader).
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// jsonAttrs кодирует map attrs в JSON-строку ("{}" для пустого).
func jsonAttrs(attrs map[string]any) (string, error) {
	if attrs == nil {
		attrs = map[string]any{}
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

// jsonUnmarshalAttrs декодирует JSON attrs в map.
func jsonUnmarshalAttrs(s string, out *map[string]any) error {
	return json.Unmarshal([]byte(s), out)
}
