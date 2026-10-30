package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	// Keep the test off the machine's real config file.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg != Default() {
		t.Errorf("Load defaults = %+v, want %+v", cfg, Default())
	}
}

func TestLoadExplicitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twig.json")
	content := "{\"context\": 10, \"ignore-whitespace\": true, \"sidebar-width\": 40}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Context != 10 {
		t.Errorf("context = %d, want 10", cfg.Context)
	}
	if !cfg.IgnoreWhitespace {
		t.Error("ignore-whitespace = false, want true")
	}
	if cfg.SidebarWidth != 40 {
		t.Errorf("sidebar-width = %d, want 40", cfg.SidebarWidth)
	}
	// Untouched keys keep their defaults.
	if !cfg.LineNumbers {
		t.Error("line-numbers = false, want default true")
	}
}

func TestLoadExplicitFileOverridesSearch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, "other.json")
	if err := os.WriteFile(path, []byte("{\"log-limit\": 9}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLimit != 9 {
		t.Errorf("log-limit = %d, want 9", cfg.LogLimit)
	}
}

func TestLoadInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twig.json")
	if err := os.WriteFile(path, []byte("{\"context\": [not,a,number]}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Error("Load should fail on an invalid config file")
	}
}

func TestLoadClamps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twig.json")
	if err := os.WriteFile(path, []byte("{\"context\": -1, \"sidebar-width\": 0, \"log-limit\": -5}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Context != 0 {
		t.Errorf("context = %d, want 0", cfg.Context)
	}
	if cfg.SidebarWidth != 10 {
		t.Errorf("sidebar-width = %d, want clamped to 10", cfg.SidebarWidth)
	}
	if cfg.LogLimit != 1 {
		t.Errorf("log-limit = %d, want clamped to 1", cfg.LogLimit)
	}
}

func TestSaveRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "twig.json")

	cfg := Default()
	cfg.SmoothScroll = true
	cfg.Context = 5
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != cfg {
		t.Fatalf("round trip = %+v, want %+v", got, cfg)
	}
}

func TestPathResolution(t *testing.T) {
	if got, err := Path("/tmp/custom.json"); err != nil || got != "/tmp/custom.json" {
		t.Fatalf("Path explicit = %q, %v", got, err)
	}

	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got, err := Path(""); err != nil || got != "/tmp/xdg/twig/twig.json" {
		t.Fatalf("Path default = %q, %v", got, err)
	}
}

func TestSmoothScrollDefaultsOn(t *testing.T) {
	if !Default().SmoothScroll {
		t.Fatal("smooth scrolling should be on by default")
	}
}

// TestWatchDebounceIsClamped covers the knob's guard rails: below a few tens of
// milliseconds one save reports as several, above two seconds every save feels
// late, so both ends are clamped rather than trusted.
func TestWatchDebounceIsClamped(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "twig.json")

	for _, tt := range []struct {
		raw  string
		want int
	}{
		{`{"watch-debounce": 1}`, 10},
		{`{"watch-debounce": 20}`, 20},
		{`{"watch-debounce": 90000}`, 2000},
		{`{"watch-debounce": 0}`, 0},
	} {
		if err := os.WriteFile(file, []byte(tt.raw), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(file)
		if err != nil {
			t.Fatalf("Load(%s): %v", tt.raw, err)
		}
		if cfg.WatchDebounce != tt.want {
			t.Errorf("%s = %d, want %d", tt.raw, cfg.WatchDebounce, tt.want)
		}
	}
}
