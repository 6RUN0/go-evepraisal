// Package esi fetches market prices from the EVE Swagger Interface.
package esi

import (
	"log"
	"strings"
)

// CompatibilityDate is sent as X-Compatibility-Date. It is the oldest date ESI
// accepts and its responses match the former /latest route.
const CompatibilityDate = "2020-01-01"

var legacyVersionSuffixes = []string{"/latest", "/v1", "/legacy"}

// NormalizeBaseURL strips surrounding spaces, trailing "/", then one trailing
// "/latest", "/v1" or "/legacy" (dated ESI has no version path); it logs a
// warning when a suffix was stripped.
func NormalizeBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range legacyVersionSuffixes {
		if strings.HasSuffix(base, suffix) {
			stripped := strings.TrimRight(strings.TrimSuffix(base, suffix), "/")
			log.Printf("WARN: esi_baseurl %q ends with %q, using %q", raw, suffix, stripped)
			return stripped
		}
	}
	return base
}
