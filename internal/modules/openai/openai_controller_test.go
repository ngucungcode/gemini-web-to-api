package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"gemini-web-to-api/internal/modules/providers"
	"github.com/gofiber/fiber/v3"
)

func TestOpenAIRequestError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		kind   string
	}{
		{"deadline", context.DeadlineExceeded, 504, "timeout_error"},
		{"wrapped deadline", fmt.Errorf("generation: %w", context.DeadlineExceeded), 504, "timeout_error"},
		{"model", &providers.ModelSelectionError{Message: "unsupported model"}, 400, "invalid_request_error"},
		{"unknown", errors.New("failure"), 500, "api_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error { return openAIRequestError(c, tc.err) })
			res, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", res.StatusCode, tc.status)
			}
			var body struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Type != tc.kind {
				t.Errorf("error type = %q, want %q", body.Error.Type, tc.kind)
			}
		})
	}
}
