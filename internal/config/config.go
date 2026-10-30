// Package config loads and validates the twig configuration file, with
// sensible defaults for when no file exists.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the twig configuration.
type Config struct {
	// Context is the number of context lines shown around hunks.
	Context int `json:"context"`
	// IgnoreWhitespace ignores whitespace changes when diffing.
	IgnoreWhitespace bool `json:"ignore-whitespace"`
	// Wrap soft-wraps long lines in the diff view.
	Wrap bool `json:"wrap"`
	// LineNumbers shows old/new line numbers in the gutter.
	LineNumbers bool `json:"line-numbers"`
	// SidebarWidth is the width of the file list sidebar in cells.
	SidebarWidth int `json:"sidebar-width"`
	// ShowLeftSidebar starts with the files pane visible.
	ShowLeftSidebar bool `json:"show-left-sidebar"`
	// ShowRightSidebar starts with the commit files pane visible.
	ShowRightSidebar bool `json:"show-right-sidebar"`
	// SmoothScroll animates large scroll jumps.
	SmoothScroll bool `json:"smooth-scroll"`
	// Watch refreshes the view when the repository changes on disk.
	Watch bool `json:"watch"`
	// WatchDebounce is how long the filesystem must be quiet before a change
	// counts, in milliseconds. Zero means the built-in default.
	WatchDebounce int `json:"watch-debounce"`
	// LogLimit is the number of commits shown in the log browser.
	LogLimit int `json:"log-limit"`
	// SplitView shows diffs as two side-by-side columns instead of one.
	SplitView bool `json:"split-view"`
}

// Default returns the built-in default configuration.
func Default() Config {
	return Config{
		Context:          3,
		IgnoreWhitespace: false,
		Wrap:             false,
		LineNumbers:      true,
		SidebarWidth:     32,
		ShowLeftSidebar:  false,
		ShowRightSidebar: false,
		SmoothScroll:     true,
		Watch:            true,
		WatchDebounce:    120,
		LogLimit:         200,
		SplitView:        false,
	}
}

// Load reads the configuration, starting with defaults and merging any
// config file found in the search paths, plus an explicit path when given.
// Values left out of the file keep their defaults.
func Load(path string) (Config, error) {
	cfg := Default()

	file := path
	if file == "" {
		dir, err := userConfigDir()
		if err != nil {
			return cfg, nil
		}
		file = filepath.Join(dir, "twig.json")
	}

	data, err := os.ReadFile(file)
	if err != nil {
		// A missing config file in the search path is fine; anything else is a
		// problem.
		if errors.Is(err, os.ErrNotExist) && path == "" {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Context < 0 {
		cfg.Context = 0
	}
	if cfg.SidebarWidth < 10 {
		cfg.SidebarWidth = 10
	}
	if cfg.LogLimit < 1 {
		cfg.LogLimit = 1
	}
	// A quiet period below a few tens of milliseconds would report one save as
	// several, and a huge one would make every save feel late.
	if cfg.WatchDebounce != 0 {
		cfg.WatchDebounce = max(min(cfg.WatchDebounce, 2000), 10)
	}

	return cfg, nil
}

// userConfigDir returns the directory twig reads its config file from:
// $XDG_CONFIG_HOME/twig when set, $HOME/.config/twig otherwise.
func userConfigDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "twig"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "twig"), nil
}

// Path resolves where twig reads and writes its config file: the explicit
// path when one was given, $XDG_CONFIG_HOME/twig/twig.json otherwise.
func Path(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "twig.json"), nil
}

// Save writes cfg to path as indented JSON, creating the parent directory.
// It writes through a temporary file so a reader never sees half a config.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".twig-*.json")
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
