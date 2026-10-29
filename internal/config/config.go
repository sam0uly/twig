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
	// SmoothScroll animates large scroll jumps.
	SmoothScroll bool `json:"smooth-scroll"`
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
		SmoothScroll:     true,
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
