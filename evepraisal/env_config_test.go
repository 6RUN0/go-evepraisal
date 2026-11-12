package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// writeSecretFile writes content to a fresh file and returns its path.
func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvNamePortable(t *testing.T) {
	want := map[string]string{
		"base-url":               "EVEPRAISAL_BASE_URL",
		"cookie-auth-key":        "EVEPRAISAL_COOKIE_AUTH_KEY",
		"https_domain-whitelist": "EVEPRAISAL_HTTPS_DOMAIN_WHITELIST",
		"db_path":                "EVEPRAISAL_DB_PATH",
	}
	for key, name := range want {
		if got := envName(key); got != name {
			t.Errorf("envName(%q) = %q, want %q", key, got, name)
		}
	}
}

func TestConfigureEnvPortableName(t *testing.T) {
	t.Setenv("EVEPRAISAL_BASE_URL", "https://portable.example")
	v := viper.New()
	if err := configureEnv(v); err != nil {
		t.Fatal(err)
	}
	if got := v.GetString("base-url"); got != "https://portable.example" {
		t.Errorf("base-url = %q, want https://portable.example", got)
	}
}

func TestConfigureEnvLegacyName(t *testing.T) {
	t.Setenv("EVEPRAISAL_BASE-URL", "https://legacy.example")
	v := viper.New()
	if err := configureEnv(v); err != nil {
		t.Fatal(err)
	}
	if got := v.GetString("base-url"); got != "https://legacy.example" {
		t.Errorf("base-url = %q, want https://legacy.example", got)
	}
}

func TestConfigureEnvEmptyVariableIsUnset(t *testing.T) {
	t.Setenv("EVEPRAISAL_SITE_NAME", "")
	v := viper.New()
	v.SetDefault("site-name", "Default")
	if err := configureEnv(v); err != nil {
		t.Fatal(err)
	}
	if got := v.GetString("site-name"); got != "Default" {
		t.Errorf("site-name = %q, want the default", got)
	}
}

func TestConfigureEnvConflictingNames(t *testing.T) {
	t.Setenv("EVEPRAISAL_BASE_URL", "https://a.example")
	t.Setenv("EVEPRAISAL_BASE-URL", "https://b.example")
	err := configureEnv(viper.New())
	if err == nil || !strings.Contains(err.Error(), "EVEPRAISAL_BASE_URL") || !strings.Contains(err.Error(), "EVEPRAISAL_BASE-URL") {
		t.Errorf("configureEnv() = %v, want an error naming both variables", err)
	}
}

func TestConfigureEnvSameValueUnderBothNames(t *testing.T) {
	t.Setenv("EVEPRAISAL_BASE_URL", "https://a.example")
	t.Setenv("EVEPRAISAL_BASE-URL", "https://a.example")
	v := viper.New()
	if err := configureEnv(v); err != nil {
		t.Fatal(err)
	}
	if got := v.GetString("base-url"); got != "https://a.example" {
		t.Errorf("base-url = %q, want https://a.example", got)
	}
}

func TestConfigureEnvFileStripsOneLineEnding(t *testing.T) {
	cases := map[string]string{
		"no newline":     "s3cret",
		"LF":             "s3cret\n",
		"CRLF":           "s3cret\r\n",
		"two LF":         "s3cret\n\n",
		"leading spaces": "  s3cret  \n",
		"empty file":     "",
	}
	want := map[string]string{
		"no newline":     "s3cret",
		"LF":             "s3cret",
		"CRLF":           "s3cret",
		"two LF":         "s3cret\n",
		"leading spaces": "  s3cret  ",
		"empty file":     "",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("EVEPRAISAL_SSO_CLIENT_SECRET_FILE", writeSecretFile(t, content))
			v := viper.New()
			v.SetDefault("sso-client-secret", "default")
			if err := configureEnv(v); err != nil {
				t.Fatal(err)
			}
			if got := v.GetString("sso-client-secret"); got != want[name] {
				t.Errorf("sso-client-secret = %q, want %q", got, want[name])
			}
		})
	}
}

func TestConfigureEnvValueAndFileConflict(t *testing.T) {
	for _, valueName := range []string{"EVEPRAISAL_BASE_URL", "EVEPRAISAL_BASE-URL"} {
		t.Run(valueName, func(t *testing.T) {
			t.Setenv(valueName, "https://a.example")
			t.Setenv("EVEPRAISAL_BASE_URL_FILE", writeSecretFile(t, "https://a.example"))
			err := configureEnv(viper.New())
			if err == nil || !strings.Contains(err.Error(), valueName) || !strings.Contains(err.Error(), "EVEPRAISAL_BASE_URL_FILE") {
				t.Errorf("configureEnv() = %v, want an error naming %s and EVEPRAISAL_BASE_URL_FILE", err, valueName)
			}
		})
	}
}

func TestConfigureEnvMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	t.Setenv("EVEPRAISAL_COOKIE_AUTH_KEY_FILE", path)
	err := configureEnv(viper.New())
	if err == nil || !strings.Contains(err.Error(), "cookie-auth-key") || !strings.Contains(err.Error(), path) {
		t.Errorf("configureEnv() = %v, want an error naming cookie-auth-key and %s", err, path)
	}
}

func TestConfigureEnvUnreadableFileHidesContent(t *testing.T) {
	path := writeSecretFile(t, "topsecret")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(path); err == nil {
		_ = f.Close()
		t.Skip("file stays readable (running as root)")
	}
	t.Setenv("EVEPRAISAL_COOKIE_AUTH_KEY_FILE", path)
	err := configureEnv(viper.New())
	if err == nil || !strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "topsecret") {
		t.Errorf("configureEnv() = %v, want an error naming %s without the content", err, path)
	}
}

func TestConfigureEnvTypedKeysFromFile(t *testing.T) {
	t.Setenv("EVEPRAISAL_HTTP_REDIRECT_FILE", writeSecretFile(t, "true\n"))
	t.Setenv("EVEPRAISAL_MARKET_FETCH_INTERVAL_FILE", writeSecretFile(t, "15\n"))
	t.Setenv("EVEPRAISAL_HTTPS_DOMAIN_WHITELIST_FILE", writeSecretFile(t, "a.example  b.example\n"))
	v := viper.New()
	if err := configureEnv(v); err != nil {
		t.Fatal(err)
	}
	if !v.GetBool("http_redirect") {
		t.Error("http_redirect = false, want true")
	}
	if got := v.GetInt("market-fetch-interval"); got != 15 {
		t.Errorf("market-fetch-interval = %d, want 15", got)
	}
	if got := v.GetStringSlice("https_domain-whitelist"); strings.Join(got, ",") != "a.example,b.example" {
		t.Errorf("https_domain-whitelist = %q, want [a.example b.example]", got)
	}
}

func TestConfigureEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	toml := "site-name = \"File\"\nsource-url = \"https://file.example\"\nfavicon-path = \"/file.ico\"\n"
	if err := os.WriteFile(filepath.Join(dir, "evepraisal.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVEPRAISAL_SITE_NAME", "Env")
	t.Setenv("EVEPRAISAL_SOURCE_URL_FILE", writeSecretFile(t, "https://env-file.example\n"))
	v := viper.New()
	v.SetDefault("site-name", "Default")
	v.SetDefault("source-url", "https://default.example")
	v.SetDefault("favicon-path", "")
	v.SetDefault("ad-block", "default-ad")
	if err := readConfig(v, dir); err != nil {
		t.Fatal(err)
	}
	if err := configureEnv(v); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"site-name":    "Env",
		"source-url":   "https://env-file.example",
		"favicon-path": "/file.ico",
		"ad-block":     "default-ad",
	}
	for key, value := range want {
		if got := v.GetString(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestBuildSettingsLogMasksSecrets(t *testing.T) {
	v := viper.New()
	v.SetDefault("newrelic_license-key", "")
	v.SetDefault("site-name", "Visible")
	for _, key := range []string{"cookie-auth-key", "cookie-encryption-key", "sso-client-secret"} {
		v.Set(key, "secret-"+key)
	}
	got := buildSettingsLog(v)
	for _, key := range []string{"cookie-auth-key", "cookie-encryption-key", "sso-client-secret"} {
		if strings.Contains(got, "secret-"+key) {
			t.Errorf("settings log prints the value of %s:\n%s", key, got)
		}
	}
	for _, line := range []string{"cookie-auth-key <set>", "sso-client-secret <set>", "newrelic_license-key <unset>", `site-name "Visible"`} {
		if !strings.Contains(strings.Join(strings.Fields(got), " "), line) {
			t.Errorf("settings log lacks %q:\n%s", line, got)
		}
	}
}

// TestConfigKeysCoverViperReads pins that every key read through viper in
// non-test code has a row in configKeys, so its environment variables exist.
func TestConfigKeysCoverViperReads(t *testing.T) {
	// All files regardless of build tags, so watch_dev.go counts too.
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	read := map[string]bool{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkgIdent, ok := sel.X.(*ast.Ident); !ok || pkgIdent.Name != "viper" {
				return true
			}
			if !strings.HasPrefix(sel.Sel.Name, "Get") && sel.Sel.Name != "SetDefault" && sel.Sel.Name != "IsSet" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			read[key] = true
			return true
		})
	}
	listed := map[string]bool{}
	for _, key := range configKeys {
		listed[key.name] = true
		if !read[key.name] {
			t.Errorf("configKeys lists %q, which no code reads", key.name)
		}
	}
	for key := range read {
		if !listed[key] {
			t.Errorf("viper key %q is missing from configKeys", key)
		}
	}
	if len(read) == 0 {
		t.Fatal("found no viper reads; the scan is broken")
	}
}

// TestReadmeListsEnvNames pins the README configuration table to configKeys.
func TestReadmeListsEnvNames(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range configKeys {
		row := "| `" + key.name + "` | `" + envName(key.name) + "` |"
		if !strings.Contains(string(readme), row) {
			t.Errorf("README.md lacks the row start %s", row)
		}
	}
}
