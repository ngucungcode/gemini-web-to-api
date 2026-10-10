package server

import (
	_ "embed"

	"github.com/gofiber/fiber/v3"
)

//go:embed static/dashboard.html
var dashboardHTML []byte

// DashboardUI serves the web dashboard interface for monitoring and managing Gemini accounts
func DashboardUI(c fiber.Ctx) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(dashboardHTML)
}
