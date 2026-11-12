package esi

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
		warns    bool
	}{
		{"https://esi.evetech.net", "https://esi.evetech.net", false},
		{"https://esi.evetech.net/", "https://esi.evetech.net", false},
		{"https://esi.evetech.net/latest", "https://esi.evetech.net", true},
		{"https://esi.evetech.net/latest/", "https://esi.evetech.net", true},
		{"https://esi.evetech.net/latest//", "https://esi.evetech.net", true},
		{"https://esi.evetech.net/v1", "https://esi.evetech.net", true},
		{"https://esi.evetech.net/legacy", "https://esi.evetech.net", true},
		{"  https://esi.evetech.net/latest ", "https://esi.evetech.net", true},
		{"https://host/latestfoo", "https://host/latestfoo", false},
		{"https://host/a/latest", "https://host/a", true},
		{"", "", false},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		orig := log.Writer()
		log.SetOutput(&buf)
		got := NormalizeBaseURL(c.in)
		log.SetOutput(orig)
		if got != c.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
		if warned := strings.Contains(buf.String(), "WARN"); warned != c.warns {
			t.Errorf("NormalizeBaseURL(%q) warned = %v, want %v (log %q)", c.in, warned, c.warns, buf.String())
		}
	}
}
