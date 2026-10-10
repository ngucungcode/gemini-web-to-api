package openai

import (
	"encoding/json"
	"gemini-web-to-api/internal/modules/openai/dto"
	"testing"
)

func TestJSONFormatContract(t *testing.T) {
	var req dto.ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"response_format":{"type":"json_object"}}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
		t.Fatal("format discarded")
	}
	for _, text := range []string{
		`{"x":1}`,
		"Explanation\n```json\n{\"x\":1}\n```",
		"Explanation\n```JSON\n{\"x\":1}\n```",
		"Explanation\n```\n{\"x\":1}\n```",
		"Explanation\r\n```json\r\n{\"x\":1}\r\n```",
		"Here is the result:\n{\"x\":1}\nHope this helps!",
	} {
		got, err := normalizeJSONObject(text)
		if err != nil || got != `{"x":1}` {
			t.Fatalf("%q %v", got, err)
		}
	}
	for _, text := range []string{`[]`, `null`, `{"x":1} {"x":2}`, "```json\n{}\n```\n```json\n{}\n```", "no JSON"} {
		if _, err := normalizeJSONObject(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}
