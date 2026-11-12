package staticdump

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"

	"github.com/evepraisal/go-evepraisal"
	"github.com/sethgrid/pester"
)

// DefaultManifestURL is the CCP manifest listing the latest SDE build.
const DefaultManifestURL = "https://developers.eveonline.com/static-data/tranquility/latest.jsonl"

// StaticDump identifies one SDE build and where to download its JSONL archive.
type StaticDump struct {
	// BuildNumber is the key of the on-disk database: db/types-<BuildNumber>.
	BuildNumber int64
	// URL is <manifest directory>/eve-online-static-data-<BuildNumber>-jsonl.zip.
	URL string
}

// FindLatestStaticDump reads the manifest and returns the first "sde" entry.
func FindLatestStaticDump(client *pester.Client, manifestURL string) (StaticDump, error) {
	base, err := url.Parse(manifestURL)
	if err != nil {
		return StaticDump{}, fmt.Errorf("parse manifest url: %w", err)
	}

	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return StaticDump{}, err
	}
	req.Header.Add("User-Agent", evepraisal.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return StaticDump{}, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return StaticDump{}, fmt.Errorf("fetch manifest: unexpected status %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Key         string          `json:"_key"`
			BuildNumber json.RawMessage `json:"buildNumber"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return StaticDump{}, fmt.Errorf("manifest line %d: %w", lineNumber, err)
		}
		if entry.Key != "sde" {
			continue
		}
		build, err := strconv.ParseInt(string(entry.BuildNumber), 10, 64)
		if err != nil || build <= 0 {
			return StaticDump{}, fmt.Errorf("manifest line %d: invalid buildNumber %q", lineNumber, entry.BuildNumber)
		}
		archive := base
		archive.Path = path.Join(path.Dir(base.Path), fmt.Sprintf("eve-online-static-data-%d-jsonl.zip", build))
		archive.RawQuery = ""
		return StaticDump{BuildNumber: build, URL: archive.String()}, nil
	}
	if err := scanner.Err(); err != nil {
		return StaticDump{}, fmt.Errorf("read manifest: %w", err)
	}
	return StaticDump{}, fmt.Errorf("manifest has no sde entry")
}
