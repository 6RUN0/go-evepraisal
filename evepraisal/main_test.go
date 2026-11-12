package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestReadConfigMissingFileIsTolerated(t *testing.T) {
	if err := readConfig(viper.New(), t.TempDir()); err != nil {
		t.Errorf("readConfig() = %v, want nil for a missing file", err)
	}
}

// TestReadConfigSyntaxErrorIsFatal pins the silent fallback to defaults on a broken file.
func TestReadConfigSyntaxErrorIsFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evepraisal.toml")
	if err := os.WriteFile(path, []byte("base-url = = broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := readConfig(viper.New(), dir)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("readConfig() = %v, want an error naming %s", err, path)
	}
}

func TestReadConfigUnreadableFileIsFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evepraisal.toml")
	if err := os.WriteFile(path, []byte("a = 1"), 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(path); err == nil {
		_ = f.Close()
		t.Skip("file stays readable (running as root)")
	}
	err := readConfig(viper.New(), dir)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("readConfig() = %v, want an error naming %s", err, path)
	}
}

func TestReadConfigValidFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "evepraisal.toml"), []byte(`site-name = "X"`), 0o600); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	if err := readConfig(v, dir); err != nil || v.GetString("site-name") != "X" {
		t.Errorf("readConfig() = %v, site-name %q", err, v.GetString("site-name"))
	}
}
