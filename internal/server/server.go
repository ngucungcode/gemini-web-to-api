package server

import (
	"context"
	"fmt"
	"time"

	"gemini-web-to-api/internal/commons/configs"
	"gemini-web-to-api/internal/modules/providers"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// New creates a new Fiber app instance
func NewGeminiWebToAPI(log *zap.Logger, cfg *configs.Config, pool *providers.Pool) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName: "Gemini Web To API",
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With", "X-Admin-Token", "x-api-key", "anthropic-version"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowCredentials: false,
	}))

	app.Use(recover.New())

	if cfg.RateLimit.Enabled {
		app.Use(limiter.New(limiter.Config{
			Max:        cfg.RateLimit.MaxRequests,
			Expiration: time.Duration(cfg.RateLimit.WindowMs) * time.Millisecond,
		}))
	}

	app.Get("/", DashboardUI)
	app.Get("/dashboard", DashboardUI)
	app.Get("/docs", ScalarUI)
	app.Get("/openapi.json", OpenAPISpec)

	app.Get("/health", HealthCheckWithPool(pool))
	app.Get("/health/accounts", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"accounts": pool.Statuses(), "total": pool.Size(), "healthy": pool.IsHealthy()})
	})
	registerAdminRoutes(app, pool, cfg, log)

	return app
}

func HealthCheck(c fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  "ok",
		"service": "gemini-web-to-api",
	})
}

func HealthCheckWithPool(pool *providers.Pool) fiber.Handler {
	return func(c fiber.Ctx) error {
		accounts := pool.Statuses()
		healthy := false
		for _, account := range accounts {
			healthy = healthy || account.Healthy
		}
		code, status, recovery := fiber.StatusOK, "ok", "none"
		if !healthy {
			code, status = fiber.StatusServiceUnavailable, "degraded"
			// Unhealthy alone cannot distinguish startup, network failure, or expired auth.
			recovery = "wait_or_refresh_session"
			if len(accounts) == 0 {
				recovery = "add_account"
			}
		}
		return c.Status(code).JSON(fiber.Map{
			"status":   status,
			"service":  "gemini-web-to-api",
			"accounts": accounts,
			"total":    len(accounts),
			"recovery": recovery,
		})
	}
}

// Register404Handler registers the 404 handler for unmatched routes
// This must be called AFTER all other routes are registered
func Register404Handler(app *fiber.App) {
	app.All("*", func(c fiber.Ctx) error {
		method := c.Method()
		path := c.Path()
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"status":  fiber.StatusNotFound,
			"error":   "Not Found",
			"message": fmt.Sprintf("Cannot %s %s", method, path),
		})
	})
}

// RegisterFiberLifecycle registers the Fiber app lifecycle hooks
func RegisterFiberLifecycle(lc fx.Lifecycle, app *fiber.App, cfg *configs.Config, log *zap.Logger) {
	port := cfg.Server.Port
	address := fmt.Sprintf(":%s", port)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			Register404Handler(app)
			log.Info("Starting server", zap.String("address", address))
			// Start server in a goroutine to not block
			go func() {
				if err := app.Listen(address); err != nil {
					log.Error("Server error", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down server")
			return app.ShutdownWithContext(ctx)
		},
	})
}
