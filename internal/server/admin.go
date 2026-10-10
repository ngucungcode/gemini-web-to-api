package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gemini-web-to-api/internal/commons/configs"
	"gemini-web-to-api/internal/commons/utils"
	"gemini-web-to-api/internal/modules/providers"

	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

type adminAccounts struct {
	pool   *providers.Pool
	cfg    *configs.Config
	log    *zap.Logger
	tokens string
}

// ADMIN_TOKEN (cfg.AdminToken) guards /admin/*: Authorization Bearer, X-Admin-Token, or ?token=.
func newAdminAccounts(pool *providers.Pool, cfg *configs.Config, log *zap.Logger) *adminAccounts {
	return &adminAccounts{pool: pool, cfg: cfg, log: log, tokens: cfg.AdminToken}
}

func (a *adminAccounts) guard(c fiber.Ctx) bool {
	if strings.TrimSpace(a.tokens) == "" {
		return true
	}
	got := strings.TrimSpace(c.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(got), "bearer ") {
		got = strings.TrimSpace(got[len("bearer "):])
	}
	if got == "" {
		got = strings.TrimSpace(c.Get("X-Admin-Token"))
	}
	if got == "" {
		got = strings.TrimSpace(c.Query("token"))
	}
	if got != a.tokens {
		_ = c.Status(fiber.StatusUnauthorized).JSON(utils.ErrorToResponse(fiber.ErrUnauthorized, "auth_error"))
		return false
	}
	return true
}

type addAccountRequest struct {
	Name     string `json:"name"`
	Cookies  string `json:"cookies"`
	AuthUser string `json:"auth_user"`
	Replace  bool   `json:"replace"`
}

func (a *adminAccounts) list(c fiber.Ctx) error {
	if !a.guard(c) {
		return nil
	}
	return c.JSON(fiber.Map{"accounts": a.pool.Statuses(), "total": a.pool.Size()})
}

func (a *adminAccounts) add(c fiber.Ctx) error {
	if !a.guard(c) {
		return nil
	}
	var req addAccountRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ErrorToResponse(err, "invalid_request_error"))
	}
	name := strings.TrimSpace(req.Name)
	cookies := strings.TrimSpace(req.Cookies)
	authUser := strings.TrimSpace(req.AuthUser)
	if cookies == "" {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ErrorToResponse(fiber.NewError(fiber.StatusBadRequest, "cookies is required (paste the complete Cookie header from the Gemini Web batchexecute request)"), "invalid_request_error"))
	}
	if name == "" {
		name = "acc" + itoa2(a.pool.Size()+1)
	}
	if existing := a.pool.Get(name); existing != nil {
		if !req.Replace {
			return c.Status(fiber.StatusConflict).JSON(utils.ErrorToResponse(fiber.NewError(fiber.StatusConflict, "account name already exists (set replace=true to rotate its cookies)"), "invalid_request_error"))
		}
		if err := existing.ReplaceCookies(cookies, authUser); err != nil {
			return c.Status(fiber.StatusBadGateway).JSON(utils.ErrorToResponse(err, "api_error"))
		}
		if err := persistAccount(a.cfg.Gemini.CookiesDir, name, cookies, authUser); err != nil {
			a.log.Warn("Account rotated but not persisted", zap.String("account", name), zap.Error(err))
		}
		return c.JSON(fiber.Map{"status": "replaced", "account": existing.AccountStatus()})
	}
	acc, err := configs.ParseAccount(name, cookies, authUser)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ErrorToResponse(err, "invalid_request_error"))
	}
	client := providers.NewClientForAccount(acc, a.cfg.Gemini.RefreshInterval, a.cfg.Gemini.MaxRetries, a.cfg.Gemini.Temporary, a.cfg.Gemini.CookiesDir, a.log)
	initCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := client.Init(initCtx); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(utils.ErrorToResponse(err, "api_error"))
	}
	a.pool.Add(client)
	if err := persistAccount(a.cfg.Gemini.CookiesDir, name, cookies, authUser); err != nil {
		a.log.Warn("Account added but not persisted", zap.String("account", name), zap.Error(err))
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"status": "added", "account": client.AccountStatus()})
}

func (a *adminAccounts) remove(c fiber.Ctx) error {
	if !a.guard(c) {
		return nil
	}
	name := strings.TrimSpace(c.Params("name"))
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ErrorToResponse(fiber.NewError(fiber.StatusBadRequest, "account name is required"), "invalid_request_error"))
	}
	removed := a.pool.Remove(name)
	if removed == nil {
		return c.Status(fiber.StatusNotFound).JSON(utils.ErrorToResponse(fiber.NewError(fiber.StatusNotFound, "account not found"), "invalid_request_error"))
	}
	_ = removed.Close()
	_ = configs.RemoveJSONAccount(a.cfg.Gemini.CookiesDir, name)
	for _, suffix := range []string{".txt", ".auth"} {
		_ = os.Remove(a.cfg.Gemini.CookiesDir + "/" + name + suffix)
	}
	return c.JSON(fiber.Map{"status": "removed", "account": name})
}

func (a *adminAccounts) ReloadFromDisk() (int, int, error) {
	fileAccounts, err := configs.LoadDirAccounts(a.cfg.Gemini.CookiesDir)
	if err != nil {
		return 0, 0, err
	}
	added := 0
	updated := 0
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, acc := range fileAccounts {
		if existing := a.pool.Get(acc.Name); existing != nil {
			if !existing.IsHealthy() || existing.AccountStatus().MaskedPSID != acc.MaskedPSID() {
				if err := existing.ReplaceCookies(acc.Cookies, acc.AuthUser); err == nil {
					updated++
				}
			}
		} else {
			client := providers.NewClientForAccount(acc, a.cfg.Gemini.RefreshInterval, a.cfg.Gemini.MaxRetries, a.cfg.Gemini.Temporary, a.cfg.Gemini.CookiesDir, a.log)
			if err := client.Init(ctx); err == nil {
				a.pool.Add(client)
				added++
			}
		}
	}
	return added, updated, nil
}

func (a *adminAccounts) reload(c fiber.Ctx) error {
	if !a.guard(c) {
		return nil
	}
	added, updated, err := a.ReloadFromDisk()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ErrorToResponse(err, "api_error"))
	}
	return c.JSON(fiber.Map{
		"status":   "reloaded",
		"added":    added,
		"updated":  updated,
		"total":    a.pool.Size(),
		"accounts": a.pool.Statuses(),
	})
}

func (a *adminAccounts) startAutoWatcher() {
	dir := strings.TrimSpace(a.cfg.Gemini.CookiesDir)
	if dir == "" {
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	var lastSnapshot string

	getSnapshot := func() string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}
		var parts []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s:%d:%d", e.Name(), info.Size(), info.ModTime().UnixNano()))
		}
		return strings.Join(parts, "|")
	}

	lastSnapshot = getSnapshot()

	for range ticker.C {
		current := getSnapshot()
		if current != "" && current != lastSnapshot {
			a.log.Info("📁 Auto-detected changes in cookies directory, automatically reloading accounts...", zap.String("dir", dir))
			lastSnapshot = current
			added, updated, err := a.ReloadFromDisk()
			if err != nil {
				a.log.Warn("Failed to auto-reload accounts from cookies dir", zap.Error(err))
			} else {
				a.log.Info("✅ Automatically reloaded accounts from cookies dir", zap.Int("added", added), zap.Int("updated", updated), zap.Int("total", a.pool.Size()))
			}
		}
	}
}

func persistAccount(dir, name, cookies, authUser string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if accounts, err := configs.ReadJSONAccounts(dir); err == nil && len(accounts) > 0 {
		acc, err := configs.ParseAccount(name, cookies, authUser)
		if err != nil {
			return err
		}
		return configs.UpsertJSONAccount(dir, acc)
	}
	if err := os.WriteFile(dir+"/"+name+".txt", []byte(strings.TrimSpace(cookies)+"\n"), 0600); err != nil {
		return err
	}
	if strings.TrimSpace(authUser) != "" {
		_ = os.WriteFile(dir+"/"+name+".auth", []byte(strings.TrimSpace(authUser)+"\n"), 0600)
	}
	return nil
}

func itoa2(n int) string {
	if n <= 0 {
		return "1"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

func (a *adminAccounts) test(c fiber.Ctx) error {
	if !a.guard(c) {
		return nil
	}
	name := strings.TrimSpace(c.Params("name"))
	var client *providers.Client
	if name != "" {
		client = a.pool.Get(name)
		if client == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Account not found", "success": false})
		}
	} else {
		if a.pool.Size() == 0 {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "No accounts configured in pool", "success": false})
		}
		statuses := a.pool.Statuses()
		for _, st := range statuses {
			if st.Healthy {
				client = a.pool.Get(st.Name)
				break
			}
		}
		if client == nil {
			client = a.pool.Get(statuses[0].Name)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := client.GenerateContent(ctx, "Ping test! Return 'PONG' only.")
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"success":    false,
			"account":    client.AccountName(),
			"error":      err.Error(),
			"latency_ms": latency,
		})
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"account":    client.AccountName(),
		"reply":      strings.TrimSpace(resp.Text),
		"latency_ms": latency,
		"model":      resp.Model,
	})
}

func registerAdminRoutes(app *fiber.App, pool *providers.Pool, cfg *configs.Config, log *zap.Logger) {
	h := newAdminAccounts(pool, cfg, log)
	g := app.Group("/admin")
	g.Get("/accounts", h.list)
	g.Post("/accounts", h.add)
	g.Post("/reload", h.reload)
	g.Delete("/accounts/:name", h.remove)
	g.Post("/test", h.test)
	g.Post("/test/:name", h.test)

	go h.startAutoWatcher()
}
