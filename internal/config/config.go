// Package config loads and validates the twig configuration file, with
// sensible defaults for when no file exists.
package config

import (
	"errors"
	"fmt"

	"github.com/spf13/viper"
)

// Config is the twig configuration.
type Config struct {
	// Context is the number of context lines shown around hunks.
	Context int `mapstructure:"context"`
	// IgnoreWhitespace ignores whitespace changes when diffing.
	IgnoreWhitespace bool `mapstructure:"ignore-whitespace"`
	// Wrap soft-wraps long lines in the diff view.
	Wrap bool `mapstructure:"wrap"`
	// LineNumbers shows old/new line numbers in the gutter.
	LineNumbers bool `mapstructure:"line-numbers"`
	// SidebarWidth is the width of the file list sidebar in cells.
	SidebarWidth int `mapstructure:"sidebar-width"`
	// SmoothScroll animates large scroll jumps.
	SmoothScroll bool `mapstructure:"smooth-scroll"`
	// LogLimit is the number of commits shown in the log browser.
	LogLimit int `mapstructure:"log-limit"`
	// SplitView shows diffs as two side-by-side columns instead of one.
	SplitView bool `mapstructure:"split-view"`
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
// viper is seeded with Default so Unmarshal always yields complete values.
func Load(path string) (Config, error) {
	cfg := Default()

	v := viper.New()
	v.SetConfigType("yaml")

	for key, val := range map[string]any{
		"context":           cfg.Context,
		"ignore-whitespace": cfg.IgnoreWhitespace,
		"wrap":              cfg.Wrap,
		"line-numbers":      cfg.LineNumbers,
		"sidebar-width":     cfg.SidebarWidth,
		"smooth-scroll":     cfg.SmoothScroll,
		"log-limit":         cfg.LogLimit,
		"split-view":        cfg.SplitView,
	} {
		v.SetDefault(key, val)
	}

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("twig")
		v.AddConfigPath("$XDG_CONFIG_HOME/twig")
		v.AddConfigPath("$HOME/.config/twig")
	}

	if err := v.ReadInConfig(); err != nil {
		// A missing config file is fine; anything else is a problem.
		if path != "" || !errors.As(err, new(viper.ConfigFileNotFoundError)) {
			return cfg, fmt.Errorf("read config: %w", err)
		}
	}

	if err := v.Unmarshal(&cfg); err != nil {
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
