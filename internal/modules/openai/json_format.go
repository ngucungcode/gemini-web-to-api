package openai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var anyFence = regexp.MustCompile("(?si)```(?:json)?\\s*[\\r\\n]+(.*?)[\\r\\n]+```")

func normalizeJSONObject(text string) (string, error) {
	text = strings.TrimSpace(text)
	var value map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &value); err == nil && value != nil {
		return text, nil
	}

	blocks := anyFence.FindAllStringSubmatch(text, -1)
	var validBlocks []string
	for _, b := range blocks {
		cand := strings.TrimSpace(b[1])
		var val map[string]json.RawMessage
		if err := json.Unmarshal([]byte(cand), &val); err == nil && val != nil {
			validBlocks = append(validBlocks, cand)
		}
	}
	if len(validBlocks) == 1 {
		return validBlocks[0], nil
	}
	if len(validBlocks) > 1 {
		return "", fmt.Errorf("provider did not return one JSON object")
	}

	// Fallback: search for outermost balanced JSON object `{ ... }`
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end != -1 && end > start {
		candidate := strings.TrimSpace(text[start : end+1])
		var val map[string]json.RawMessage
		if err := json.Unmarshal([]byte(candidate), &val); err == nil && val != nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("provider did not return one JSON object")
}
