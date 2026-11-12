package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/viper"
)

const envPrefix = "EVEPRAISAL_"

// envFileSuffix marks a variable that holds the path of a file with the value
// (Docker secrets style).
const envFileSuffix = "_FILE"

// configKey is one config key the environment can set.
type configKey struct {
	name string
	// isSecret keeps the value out of the startup settings log.
	isSecret bool
	// isValueRequired rejects an empty _FILE: an empty path or number is a
	// mistake, not a choice.
	isValueRequired bool
}

// secretNameParts mask any setting whose name contains one of them, keys
// unknown to configKeys included.
var secretNameParts = []string{"key", "secret", "password", "token"}

// configKeys lists every key read through viper; TestConfigKeysCoverViperReads
// fails when a read key is missing, TestReadmeListsEnvNames when README lags.
var configKeys = []configKey{
	{name: "about-contacts-html"},
	{name: "ad-block"},
	{name: "backup_path", isValueRequired: true},
	{name: "base-url"},
	{name: "cookie-auth-key", isSecret: true},
	{name: "cookie-encryption-key", isSecret: true},
	{name: "db_path", isValueRequired: true},
	{name: "esi_baseurl"},
	{name: "extra-html-header"},
	{name: "extra-js"},
	{name: "extra-static-file-path"},
	{name: "favicon-path"},
	{name: "http_addr"},
	{name: "http_redirect", isValueRequired: true},
	{name: "https_addr"},
	{name: "https_domain-whitelist"},
	{name: "letsencrypt_email"},
	{name: "management_addr"},
	{name: "market-fetch-interval", isValueRequired: true},
	{name: "newrelic_app-name"},
	{name: "newrelic_license-key", isSecret: true},
	{name: "site-name"},
	{name: "source-url"},
	{name: "sso-authorize-url"},
	{name: "sso-client-id"},
	{name: "sso-client-secret", isSecret: true},
	{name: "sso-token-url"},
	{name: "sso-verify-url"},
}

// envName returns the portable variable name of key: dashes become
// underscores, so a POSIX shell can export it.
func envName(key string) string {
	return envPrefix + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
}

// legacyEnvName returns the variable name viper's AutomaticEnv derived from
// key, dashes kept; existing deployments set these.
func legacyEnvName(key string) string {
	return envPrefix + strings.ToUpper(key)
}

// configureEnv sets every key in configKeys that the environment provides on
// v, above the config file and the defaults. Call it after readConfig.
// EVEPRAISAL_* variables that match no key are logged as warnings, by name only.
func configureEnv(v *viper.Viper) error {
	for _, name := range findUnknownEnvNames(os.Environ()) {
		log.Printf("WARN: Unknown environment variable ignored name=%s", name)
	}
	for _, key := range configKeys {
		value, isSet, err := fetchEnvValue(key.name)
		if err != nil {
			return err
		}
		if isSet && value == "" && key.isValueRequired {
			return fmt.Errorf("%s: %s names an empty file", key.name, envName(key.name)+envFileSuffix)
		}
		if isSet {
			v.Set(key.name, value)
		}
	}
	return nil
}

// findUnknownEnvNames returns the non-empty EVEPRAISAL_* variables of environ
// (os.Environ format) that are no accepted name of any key in configKeys.
func findUnknownEnvNames(environ []string) []string {
	known := map[string]bool{}
	for _, key := range configKeys {
		known[envName(key.name)] = true
		known[legacyEnvName(key.name)] = true
		known[envName(key.name)+envFileSuffix] = true
	}
	var unknown []string
	for _, entry := range environ {
		name, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, envPrefix) && value != "" && !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// fetchEnvValue returns the value of key from its portable or legacy variable
// or from the file named by <portable>_FILE. An empty variable counts as unset.
func fetchEnvValue(key string) (string, bool, error) {
	portable, legacy, fileVar := envName(key), legacyEnvName(key), envName(key)+envFileSuffix
	valueVar, value := "", ""
	// For a key without '-' both names are the same variable, read twice.
	for _, name := range []string{portable, legacy} {
		current := os.Getenv(name)
		if current == "" {
			continue
		}
		if valueVar != "" && current != value {
			return "", false, fmt.Errorf("%s and %s are both set with different values; set only %s", valueVar, name, portable)
		}
		valueVar, value = name, current
	}
	path := os.Getenv(fileVar)
	switch {
	case path == "":
		return value, valueVar != "", nil
	case valueVar != "":
		return "", false, fmt.Errorf("%s and %s are both set; set only one", valueVar, fileVar)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		// The *PathError names the path only, never the content.
		return "", false, fmt.Errorf("%s: read %s: %w", key, fileVar, err)
	}
	text := string(content)
	if trimmed, found := strings.CutSuffix(text, "\r\n"); found {
		return trimmed, true, nil
	}
	return strings.TrimSuffix(text, "\n"), true, nil
}

// isSecretSetting reports whether the value of setting name must stay out of
// the log: flagged in configKeys or named like a credential.
func isSecretSetting(name string, secrets map[string]bool) bool {
	if secrets[name] {
		return true
	}
	lower := strings.ToLower(name)
	for _, part := range secretNameParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// buildSettingsLog renders the settings of v for the startup log, one key per
// line in key order, nested keys flattened with '.'; secret values appear as
// <set> or <unset>, and secret keys of configKeys are listed even when unset.
func buildSettingsLog(v *viper.Viper) string {
	secrets := map[string]bool{}
	listed := map[string]bool{}
	for _, key := range configKeys {
		if key.isSecret {
			secrets[key.name] = true
			listed[key.name] = true
		}
	}
	for _, name := range v.AllKeys() {
		listed[name] = true
	}
	names := make([]string, 0, len(listed))
	for name := range listed {
		names = append(names, name)
	}
	sort.Strings(names)

	buf := bytes.NewBufferString("Config settings\n")
	w := tabwriter.NewWriter(buf, 0, 0, 1, ' ', 0)
	for _, name := range names {
		value := v.Get(name)
		shown := fmt.Sprintf("%#v", value)
		if isSecretSetting(name, secrets) {
			shown = "<unset>"
			if value != nil && fmt.Sprint(value) != "" {
				shown = "<set>"
			}
		}
		// Writes into a bytes.Buffer cannot fail.
		_, _ = fmt.Fprintf(w, "\t%s\t%s\n", name, shown)
	}
	_ = w.Flush()
	return buf.String()
}
