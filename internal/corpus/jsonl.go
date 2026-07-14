package corpus

import (
	"encoding/json"
	"fmt"
	"strings"
)

// eachJSONL yields one Doc per non-empty JSONL line, extracting the text
// with a dotted field path. A missing or non-string field is a hard error
// with the exact file:line — a contamination check that silently drops
// records would understate leakage, which is the one failure mode this
// tool must not have.
func eachJSONL(path string, data []byte, field string, st *Stats, fn func(Doc) error) error {
	for i, line := range splitLines(data) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return fmt.Errorf("%s:%d: invalid JSON: %v", path, i+1, err)
		}
		text, err := extractField(rec, field)
		if err != nil {
			return fmt.Errorf("%s:%d: %v", path, i+1, err)
		}
		if strings.TrimSpace(text) == "" {
			st.EmptySkipped++
			continue
		}
		st.Docs++
		if err := fn(Doc{ID: fmt.Sprintf("%s:%d", path, i+1), Text: text}); err != nil {
			return err
		}
	}
	return nil
}

// extractField walks a dotted path through nested JSON objects and returns
// the string value at the leaf.
func extractField(rec any, field string) (string, error) {
	cur := rec
	parts := strings.Split(field, ".")
	for i, part := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("field %q: %q is not an object", field, strings.Join(parts[:i], "."))
		}
		cur, ok = obj[part]
		if !ok {
			return "", fmt.Errorf("field %q not found", field)
		}
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("field %q is not a string", field)
	}
	return s, nil
}
