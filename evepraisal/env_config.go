package main

import (
	"bytes"
	"fmt"
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
}

// configKeys lists every key read through viper; TestConfigKeysCoverViperReads
// fails when a read key is missing, TestReadmeListsEnvNames when README lags.
var configKeys = []configKey{
	{name: "about-contacts-html"},
	{name: "ad-block"},
	{name: "backup_path"},
	{name: "base-url"},
	{name: "cookie-auth-key", isSecret: true},
	{name: "cookie-encryption-key", isSecret: true},
	{name: "db_path"},
	{name: "esi_baseurl"},
	{name: "extra-html-header"},
	{name: "extra-js"},
	{name: "extra-static-file-path"},
	{name: "favicon-path"},
	{name: "http_addr"},
	{name: "http_redirect"},
	{name: "https_addr"},
	{name: "https_domain-whitelist"},
	{name: "letsencrypt_email"},
	{name: "management_addr"},
	{name: "market-fetch-interval"},
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
func configureEnv(v *viper.Viper) error {
	for _, key := range configKeys {
		value, isSet, err := fetchEnvValue(key.name)
		if err != nil {
			return err
		}
		if isSet {
			v.Set(key.name, value)
		}
	}
	return nil
}

// fetchEnvValue returns the value of key from its portable or legacy variable
// or from the file named by <portable>_FILE. An empty variable counts as unset.
func fetchEnvValue(key string) (string, bool, error) {
	portable, legacy, fileVar := envName(key), legacyEnvName(key), envName(key)+envFileSuffix
	valueVar, value := "", ""
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

// buildSettingsLog renders the settings of v for the startup log, one key per
// line in key order; secret values appear as <set> or <unset>.
func buildSettingsLog(v *viper.Viper) string {
	secrets := map[string]bool{}
	for _, key := range configKeys {
		if key.isSecret {
			secrets[key.name] = true
		}
	}
	settings := v.AllSettings()
	names := make([]string, 0, len(settings))
	for name := range settings {
		names = append(names, name)
	}
	sort.Strings(names)

	buf := bytes.NewBufferString("Config settings\n")
	w := tabwriter.NewWriter(buf, 0, 0, 1, ' ', 0)
	for _, name := range names {
		shown := fmt.Sprintf("%#v", settings[name])
		if secrets[name] {
			shown = "<unset>"
			if v.GetString(name) != "" {
				shown = "<set>"
			}
		}
		// Writes into a bytes.Buffer cannot fail.
		_, _ = fmt.Fprintf(w, "\t%s\t%s\n", name, shown)
	}
	_ = w.Flush()
	return buf.String()
}
