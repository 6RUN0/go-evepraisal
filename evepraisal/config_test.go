package main

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestBrandingDefaults(t *testing.T) {
	want := map[string]string{
		"site-name":           "Evepraisal",
		"source-url":          "https://github.com/evepraisal/go-evepraisal",
		"about-contacts-html": `<li>Send an in-game mail to: <a href="https://gate.eveonline.com/Profile/Magerawr" target="_blank">Magerawr</a></li>` + "\n" + `<li>Open an <a href="https://github.com/evepraisal/go-evepraisal/issues?state=open" target="_blank">issue on github</a></li>` + "\n" + `<li>Talk to <a href="https://tweetfleet.slack.com/messages/@sudorandom" target="_blank">@sudorandom</a> in the <a href="https://tweetfleet.slack.com/messages/C03CGPX82" target="_blank">#devfleet channel on the tweetfleet slack</a></li>` + "\n" + `<li>Mention <a href="https://twitter.com/sudorandom" target="_blank">@sudorandom</a> on twitter.</a></li>`,
		"favicon-path":        "",
	}
	for key, value := range want {
		if got := viper.GetString(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

// setViper overrides key for one test; viper has no unset, and a nil
// override falls through to the environment and the default again.
func setViper(t *testing.T, key string, value interface{}) {
	t.Helper()
	viper.Set(key, value)
	t.Cleanup(func() { viper.Set(key, nil) })
}

func TestMarketFetchIntervalDefault(t *testing.T) {
	got, err := marketFetchInterval()
	if err != nil || got != 30*time.Minute {
		t.Errorf("marketFetchInterval() = %v, %v, want 30m", got, err)
	}
}

func TestMarketFetchIntervalFromInt(t *testing.T) {
	setViper(t, "market-fetch-interval", 10)
	got, err := marketFetchInterval()
	if err != nil || got != 10*time.Minute {
		t.Errorf("marketFetchInterval() = %v, %v, want 10m", got, err)
	}
}

func TestMarketFetchIntervalFromEnv(t *testing.T) {
	clearEvepraisalEnv(t)
	t.Setenv("EVEPRAISAL_MARKET-FETCH-INTERVAL", "10")
	t.Cleanup(func() { viper.Set("market-fetch-interval", nil) })
	if err := configureEnv(viper.GetViper()); err != nil {
		t.Fatal(err)
	}
	got, err := marketFetchInterval()
	if err != nil || got != 10*time.Minute {
		t.Errorf("marketFetchInterval() = %v, %v, want 10m", got, err)
	}
}

func TestMarketFetchIntervalInvalid(t *testing.T) {
	for _, value := range []string{"0", "-5", "10m", "1.5", "abc", "", "99999999999999999999", "999999999999"} {
		setViper(t, "market-fetch-interval", value)
		got, err := marketFetchInterval()
		if err == nil || !strings.Contains(err.Error(), "market-fetch-interval") {
			t.Errorf("marketFetchInterval() with %q = %v, %v, want an error naming market-fetch-interval", value, got, err)
		}
	}
}

func TestEsiBaseURLDefaultHasNoLatest(t *testing.T) {
	if got := viper.GetString("esi_baseurl"); got != "https://esi.evetech.net" {
		t.Errorf("esi_baseurl = %q, want https://esi.evetech.net", got)
	}
}
