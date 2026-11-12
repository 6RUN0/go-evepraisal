package main

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/evepraisal/go-evepraisal/web"
	"github.com/spf13/viper"
)

func init() {
	viper.SetDefault("base-url", "http://127.0.0.1:8080")
	viper.SetDefault("http_addr", ":8080")
	viper.SetDefault("http_redirect", false)
	viper.SetDefault("https_addr", "")
	viper.SetDefault("https_domain-whitelist", []string{"evepraisal.com"})
	viper.SetDefault("letsencrypt_email", "")
	viper.SetDefault("db_path", "db/")
	viper.SetDefault("backup_path", "db/backups/")
	viper.SetDefault("esi_baseurl", "https://esi.evetech.net")
	viper.SetDefault("market-fetch-interval", 30)
	viper.SetDefault("newrelic_app-name", "Evepraisal")
	viper.SetDefault("newrelic_license-key", "")
	viper.SetDefault("management_addr", "127.0.0.1:8090")
	viper.SetDefault("extra-html-header", "")
	viper.SetDefault("extra-js", "")
	viper.SetDefault("site-name", web.DefaultSiteName)
	viper.SetDefault("source-url", web.DefaultSourceURL)
	viper.SetDefault("about-contacts-html", string(web.DefaultAboutContactsHTML))
	viper.SetDefault("favicon-path", "")
	viper.SetDefault("ad-block", "")
	viper.SetDefault("extra-static-file-path", "")
	viper.SetDefault("sso-authorize-url", "https://login.eveonline.com/oauth/authorize")
	viper.SetDefault("sso-token-url", "https://login.eveonline.com/oauth/token")
	viper.SetDefault("sso-verify-url", "https://login.eveonline.com/oauth/verify")
	viper.SetDefault("sso-verify-url", "https://login.eveonline.com/oauth/verify")

}

// configureEnv lets EVEPRAISAL_<KEY> environment variables override config keys.
func configureEnv() {
	viper.SetEnvPrefix("evepraisal")
	viper.AutomaticEnv()
}

// marketFetchInterval returns market-fetch-interval, a whole number of minutes above zero.
func marketFetchInterval() (time.Duration, error) {
	raw := viper.GetString("market-fetch-interval")
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes <= 0 || minutes > int(math.MaxInt64/int64(time.Minute)) {
		return 0, fmt.Errorf("market-fetch-interval must be a whole number of minutes above zero, got %q", raw)
	}
	return time.Duration(minutes) * time.Minute, nil
}
