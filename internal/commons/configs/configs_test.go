package configs

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	testHeaderA = "NID=rollout; __Secure-1PSID=psid-a; __Secure-1PSIDTS=ts-a; SIDCC=flags"
	testHeaderB = "NID=rollout; __Secure-1PSID=psid-b; __Secure-1PSIDTS=ts-b"
)

func clearGeminiEnv(t *testing.T) {
	t.Helper()
	// New loads .env from the working directory; never consume local credentials in tests.
	t.Chdir(t.TempDir())
	t.Setenv("GEMINI_COOKIES", "")
	t.Setenv("GEMINI_COOKIES_1", "")
	for _, n := range []string{"2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		t.Setenv("GEMINI_COOKIES_"+n, "")
		t.Setenv("GEMINI_AUTH_USER_"+n, "")
	}
	t.Setenv("GEMINI_AUTH_USER", "")
	t.Setenv("GEMINI_AUTH_USER_1", "")
	t.Setenv("GEMINI_ACCOUNTS_JSON", "")
	t.Setenv("GEMINI_COOKIES_DIR", t.TempDir())
	t.Setenv("GEMINI_1PSID", "")
	t.Setenv("GEMINI_1PSIDTS", "")
}

func TestCookieValueExtractsAuthFromFullHeader(t *testing.T) {
	header := "NID=rollout; __Secure-1PSID=account=value; __Secure-1PSIDTS=timestamp; SIDCC=flags"
	if got := cookieValue(header, "__Secure-1PSID"); got != "account=value" {
		t.Fatalf("1PSID = %q", got)
	}
	if got := cookieValue(header, "__Secure-1PSIDTS"); got != "timestamp" {
		t.Fatalf("1PSIDTS = %q", got)
	}
}

func TestParseAccountValid(t *testing.T) {
	acc, err := ParseAccount("acc1", testHeaderA, "2")
	if err != nil {
		t.Fatal(err)
	}
	if acc.Secure1PSID != "psid-a" || acc.Secure1PSIDTS != "ts-a" || acc.AuthUser != "2" {
		t.Fatalf("bad parse: %#v", acc)
	}
}

func TestParseAccountRejectsBadInput(t *testing.T) {
	if _, err := ParseAccount("x", testHeaderA, "account-two"); err == nil {
		t.Fatal("expected invalid auth slot error")
	}
	if _, err := ParseAccount("x", "NID=rollout; __Secure-1PSID=only", ""); err == nil {
		t.Fatal("expected incomplete cookie header error")
	}
}

func TestNewZeroAccountsBootsDegraded(t *testing.T) {
	clearGeminiEnv(t)
	cfg, err := New()
	if err != nil {
		t.Fatalf("zero accounts must boot degraded, got %v", err)
	}
	if len(cfg.Gemini.Accounts) != 0 {
		t.Fatalf("want 0 accounts, got %d", len(cfg.Gemini.Accounts))
	}
}

func TestNewSingleAccount(t *testing.T) {
	clearGeminiEnv(t)
	t.Setenv("GEMINI_COOKIES", testHeaderA)
	t.Setenv("GEMINI_AUTH_USER", "2")
	cfg, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Gemini.Accounts) != 1 {
		t.Fatalf("want 1 account, got %d", len(cfg.Gemini.Accounts))
	}
	a := cfg.Gemini.Accounts[0]
	if a.Secure1PSID != "psid-a" || a.Secure1PSIDTS != "ts-a" || a.AuthUser != "2" {
		t.Fatalf("bad account: %#v", a)
	}
}

func TestNewNumberedAccounts(t *testing.T) {
	clearGeminiEnv(t)
	t.Setenv("GEMINI_COOKIES", testHeaderA)
	t.Setenv("GEMINI_COOKIES_2", testHeaderB)
	t.Setenv("GEMINI_AUTH_USER_2", "1")
	cfg, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Gemini.Accounts) != 2 {
		t.Fatalf("want 2 accounts, got %d", len(cfg.Gemini.Accounts))
	}
	if cfg.Gemini.Accounts[1].AuthUser != "1" {
		t.Fatalf("bad second account: %#v", cfg.Gemini.Accounts[1])
	}
}

func TestNewDedupesSamePSID(t *testing.T) {
	clearGeminiEnv(t)
	t.Setenv("GEMINI_COOKIES", testHeaderA)
	t.Setenv("GEMINI_COOKIES_2", testHeaderA)
	cfg, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Gemini.Accounts) != 1 {
		t.Fatalf("want deduped 1 account, got %d", len(cfg.Gemini.Accounts))
	}
}

func TestLoadDirAccounts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "acc1.txt"), []byte(testHeaderA), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acc2_u1.txt"), []byte(testHeaderB), 0600); err != nil {
		t.Fatal(err)
	}
	accs, err := LoadDirAccounts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 2 {
		t.Fatalf("want 2 file accounts, got %d", len(accs))
	}
	byName := map[string]GeminiAccount{}
	for _, a := range accs {
		byName[a.Name] = a
	}
	if byName["acc2"].AuthUser != "1" {
		t.Fatalf("suffix auth not parsed: %#v", byName["acc2"])
	}
}

func TestWriteAccountsFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := []GeminiAccount{
		{Name: "a1", Cookies: testHeaderA, AuthUser: "2", Secure1PSID: "psid-a", Secure1PSIDTS: "ts-a"},
	}
	if err := WriteAccountsFile(dir, in); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDirAccounts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "a1" || got[0].AuthUser != "2" {
		t.Fatalf("roundtrip failed: %#v", got)
	}
}
