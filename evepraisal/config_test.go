package main

import (
	"testing"

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
