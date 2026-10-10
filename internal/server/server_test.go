package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"gemini-web-to-api/internal/commons/configs"
	"gemini-web-to-api/internal/modules/providers"
	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

func TestHealthRecoveryMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		clients  []*providers.Client
		recovery string
	}{
		{name: "unconfigured", recovery: "add_account"},
		{name: "not_ready", clients: []*providers.Client{providers.NewClientForAccount(configs.GeminiAccount{Name: "test", Secure1PSID: "fixture-private-cookie"}, 1, 1, false, "", zap.NewNop())}, recovery: "wait_or_refresh_session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/health", HealthCheckWithPool(providers.NewPool(tc.clients, zap.NewNop())))
			res, err := app.Test(httptest.NewRequest("GET", "/health", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var body struct {
				Status   string                    `json:"status"`
				Recovery string                    `json:"recovery"`
				Total    int                       `json:"total"`
				Accounts []providers.AccountStatus `json:"accounts"`
			}
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 503 || body.Status != "degraded" {
				t.Fatalf("status=%d body=%+v", res.StatusCode, body)
			}
			if body.Recovery != tc.recovery {
				t.Errorf("recovery=%q, want %q", body.Recovery, tc.recovery)
			}
			if body.Total != len(body.Accounts) || body.Total != len(tc.clients) {
				t.Fatalf("inconsistent account total: %+v", body)
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "fixture-private-cookie") {
				t.Fatal("health leaked credential")
			}
		})
	}
}

func TestDashboardUI(t *testing.T) {
	app := fiber.New()
	app.Get("/", DashboardUI)
	app.Get("/dashboard", DashboardUI)

	for _, path := range []string{"/", "/dashboard"} {
		req := httptest.NewRequest("GET", path, nil)
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("request to %s failed: %v", path, err)
		}
		if res.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", res.StatusCode)
		}
		ct := res.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("expected text/html Content-Type, got %s", ct)
		}
	}
}

