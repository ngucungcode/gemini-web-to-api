package configs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Gemini     GeminiConfig
	Claude     ClaudeConfig
	OpenAI     OpenAIConfig
	Server     ServerConfig
	RateLimit  RateLimitConfig
	LogLevel   string
	AdminToken string
}

type RateLimitConfig struct {
	Enabled     bool
	WindowMs    int
	MaxRequests int
}

type GeminiConfig struct {
	Accounts        []GeminiAccount
	CookiesDir      string
	RefreshInterval int
	MaxRetries      int
	Temporary       bool
}

type GeminiAccount struct {
	Name          string
	Cookies       string
	AuthUser      string
	Secure1PSID   string
	Secure1PSIDTS string
}

func (a GeminiAccount) MaskedPSID() string {
	psid := strings.TrimSpace(a.Secure1PSID)
	if len(psid) <= 4 {
		return "…"
	}
	return psid[:4] + "…"
}

type ClaudeConfig struct {
	APIKey  string
	Model   string
	Cookies string
}

type OpenAIConfig struct {
	APIKey  string
	Model   string
	Cookies string
}

type ServerConfig struct {
	Port string
}

const (
	defaultServerPort            = "4981"
	defaultGeminiRefreshInterval = 5
	defaultGeminiMaxRetries      = 3
	defaultLogLevel              = "info"
	defaultCookiesDir            = "cookies"
	maxNumberedAccounts          = 10
	accountsFileName             = "accounts.json"
)

var accountAuthSuffix = regexp.MustCompile(`_u(\d+)$`)

type accountFileEntry struct {
	Name     string `json:"name"`
	Cookies  string `json:"cookies"`
	AuthUser string `json:"auth_user"`
}

func New() (*Config, error) {
	_ = godotenv.Load()

	var cfg Config

	cfg.Server.Port = getEnv("PORT", defaultServerPort)

	cfg.LogLevel = getEnv("LOG_LEVEL", defaultLogLevel)
	cfg.AdminToken = strings.TrimSpace(os.Getenv("ADMIN_TOKEN"))

	cfg.RateLimit.Enabled = getEnvBool("RATE_LIMIT_ENABLED", false)
	cfg.RateLimit.WindowMs = getEnvInt("RATE_LIMIT_WINDOW_MS", 60000)
	cfg.RateLimit.MaxRequests = getEnvInt("RATE_LIMIT_MAX_REQUESTS", 10)

	cfg.Gemini.CookiesDir = strings.TrimSpace(getEnv("GEMINI_COOKIES_DIR", defaultCookiesDir))
	cfg.Gemini.RefreshInterval = getEnvInt("GEMINI_REFRESH_INTERVAL", defaultGeminiRefreshInterval)
	cfg.Gemini.MaxRetries = getEnvInt("GEMINI_MAX_RETRIES", defaultGeminiMaxRetries)
	cfg.Gemini.Temporary = getEnvBool("GEMINI_TEMPORARY", false)

	accounts, err := loadGeminiAccounts(cfg.Gemini.CookiesDir)
	if err != nil {
		return nil, err
	}
	cfg.Gemini.Accounts = accounts

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func loadGeminiAccounts(dir string) ([]GeminiAccount, error) {
	var accounts []GeminiAccount
	seen := make(map[string]bool)
	add := func(acc GeminiAccount) {
		if acc.Secure1PSID == "" || seen[acc.Secure1PSID] {
			return
		}
		seen[acc.Secure1PSID] = true
		accounts = append(accounts, acc)
	}

	for i := 1; i <= maxNumberedAccounts; i++ {
		suffix := ""
		name := "env"
		authKey := "GEMINI_AUTH_USER"
		if i > 1 {
			suffix = strconv.Itoa(i)
			name = "env" + suffix
			authKey = "GEMINI_AUTH_USER_" + suffix
		}
		headerKey := "GEMINI_COOKIES"
		if suffix != "" {
			headerKey += "_" + suffix
		}
		header := strings.TrimSpace(os.Getenv(headerKey))
		authVal := strings.TrimSpace(os.Getenv(authKey))
		if i == 1 {
			if header == "" {
				header = strings.TrimSpace(os.Getenv("GEMINI_COOKIES_1"))
			}
			if authVal == "" {
				authVal = strings.TrimSpace(os.Getenv("GEMINI_AUTH_USER_1"))
			}
		}
		if header == "" {
			continue
		}
		acc, err := ParseAccount(name, header, authVal)
		if err != nil {
			return nil, fmt.Errorf("account %q: %w", name, err)
		}
		add(acc)
	}

	if raw := strings.TrimSpace(os.Getenv("GEMINI_ACCOUNTS_JSON")); raw != "" {
		var entries []accountFileEntry
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return nil, fmt.Errorf("invalid GEMINI_ACCOUNTS_JSON: %w", err)
		}
		for i, e := range entries {
			name := strings.TrimSpace(e.Name)
			if name == "" {
				name = fmt.Sprintf("json%d", i+1)
			}
			if strings.TrimSpace(e.Cookies) == "" {
				continue
			}
			acc, err := ParseAccount(name, e.Cookies, e.AuthUser)
			if err != nil {
				return nil, fmt.Errorf("GEMINI_ACCOUNTS_JSON[%d]: %w", i, err)
			}
			add(acc)
		}
	}

	if strings.TrimSpace(dir) != "" {
		fileAccounts, err := LoadDirAccounts(dir)
		if err != nil {
			return nil, err
		}
		for _, acc := range fileAccounts {
			add(acc)
		}
	}

	return accounts, nil
}

func ParseAccount(name, header, authUser string) (GeminiAccount, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "acc1"
	}
	authUser = strings.TrimSpace(authUser)
	if authUser != "" {
		if n, err := strconv.Atoi(authUser); err != nil || n < 0 {
			return GeminiAccount{}, fmt.Errorf("invalid auth user value %q (must be a non-negative account slot, e.g. 2 for /u/2/)", authUser)
		}
	}
	acc := GeminiAccount{
		Name:     name,
		Cookies:  strings.TrimSpace(header),
		AuthUser: authUser,
	}
	acc.Secure1PSID = cookieValue(acc.Cookies, "__Secure-1PSID")
	acc.Secure1PSIDTS = cookieValue(acc.Cookies, "__Secure-1PSIDTS")
	if acc.Secure1PSID == "" || acc.Secure1PSIDTS == "" {
		return GeminiAccount{}, fmt.Errorf("cookies must contain __Secure-1PSID and __Secure-1PSIDTS (copy the complete Cookie header from a Gemini Web batchexecute request)")
	}
	return acc, nil
}

func LoadDirAccounts(dir string) ([]GeminiAccount, error) {
	var accounts []GeminiAccount

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cookies dir %q: %w", dir, err)
	}

	if raw, err := os.ReadFile(filepath.Join(dir, accountsFileName)); err == nil && strings.TrimSpace(string(raw)) != "" {
		var fileEntries []accountFileEntry
		if err := json.Unmarshal(raw, &fileEntries); err != nil {
			return nil, fmt.Errorf("invalid %s/%s: %w", dir, accountsFileName, err)
		}
		for i, e := range fileEntries {
			if strings.TrimSpace(e.Cookies) == "" {
				continue
			}
			name := strings.TrimSpace(e.Name)
			if name == "" {
				name = fmt.Sprintf("file%d", i+1)
			}
			acc, err := ParseAccount(name, e.Cookies, e.AuthUser)
			if err != nil {
				return nil, fmt.Errorf("%s/%s[%d]: %w", dir, accountsFileName, i, err)
			}
			accounts = append(accounts, acc)
		}
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		if strings.HasPrefix(base, ".") {
			continue
		}
		if base == accountsFileName || base == ".gitkeep" {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(base), ".txt") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, base))
		if err != nil {
			return nil, fmt.Errorf("read %s/%s: %w", dir, base, err)
		}
		header := strings.TrimSpace(string(raw))
		if header == "" {
			continue
		}
		name := strings.TrimSuffix(base, filepath.Ext(base))
		auth := ""
		if m := accountAuthSuffix.FindStringSubmatch(name); len(m) == 2 {
			auth = m[1]
			name = strings.TrimSuffix(name[:len(name)-len(m[0])], "_")
		}
		if side, err := os.ReadFile(filepath.Join(dir, name+".auth")); err == nil {
			if v := strings.TrimSpace(string(side)); v != "" {
				auth = v
			}
		}
		acc, err := ParseAccount(name, header, auth)
		if err != nil {
			return nil, fmt.Errorf("cookies file %s: %w", base, err)
		}
		accounts = append(accounts, acc)
	}

	return accounts, nil
}

func WriteAccountsFile(dir string, accounts []GeminiAccount) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("cookies dir is empty")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	entries := make([]accountFileEntry, 0, len(accounts))
	for _, a := range accounts {
		entries = append(entries, accountFileEntry{Name: a.Name, Cookies: a.Cookies, AuthUser: a.AuthUser})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, accountsFileName), append(data, '\n'), 0600)
}

func cookieValue(header, wanted string) string {
	for _, pair := range strings.Split(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(name) == wanted {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Config) Validate() error {
	// Zero accounts allowed: boot degraded, add via cookies/*.txt or POST /admin/accounts.
	if c.Server.Port == "" {
		c.Server.Port = defaultServerPort
	}

	if _, err := strconv.Atoi(c.Server.Port); err != nil {
		return fmt.Errorf("invalid PORT value: %q (must be a number)", c.Server.Port)
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func AccountsFilePath(dir string) string {
	return filepath.Join(dir, accountsFileName)
}

func ReadJSONAccounts(dir string) ([]GeminiAccount, error) {
	raw, err := os.ReadFile(AccountsFilePath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	var entries []accountFileEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", AccountsFilePath(dir), err)
	}
	accounts := make([]GeminiAccount, 0, len(entries))
	for i, e := range entries {
		if strings.TrimSpace(e.Cookies) == "" {
			continue
		}
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = fmt.Sprintf("file%d", i+1)
		}
		acc, err := ParseAccount(name, e.Cookies, e.AuthUser)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", AccountsFilePath(dir), i, err)
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}

func UpsertJSONAccount(dir string, acc GeminiAccount) error {
	accounts, err := ReadJSONAccounts(dir)
	if err != nil {
		return err
	}
	found := false
	for i, a := range accounts {
		if a.Name == acc.Name {
			accounts[i] = acc
			found = true
		}
	}
	if !found {
		accounts = append(accounts, acc)
	}
	return WriteAccountsFile(dir, accounts)
}

func RemoveJSONAccount(dir, name string) error {
	if _, err := os.Stat(AccountsFilePath(dir)); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	accounts, err := ReadJSONAccounts(dir)
	if err != nil {
		return err
	}
	kept := accounts[:0]
	for _, a := range accounts {
		if a.Name != name {
			kept = append(kept, a)
		}
	}
	return WriteAccountsFile(dir, kept)
}
