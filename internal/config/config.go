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
	// Borderless removes pane borders and separates panes with one blank cell.
	Borderless bool `json:"borderless"`
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
	// Follow moves the pane to the line that changed when a live refresh brings
	// in new content, so a file can be watched while something else edits it.
	Follow bool `json:"follow"`
	// WatchDebounce is how long the filesystem must be quiet before a change
	// counts, in milliseconds. Zero means the built-in default.
	WatchDebounce int `json:"watch-debounce"`
	// LogLimit is the number of commits shown in the log browser.
	LogLimit int `json:"log-limit"`
	// SplitView shows diffs as two side-by-side columns instead of one.
	SplitView bool `json:"split-view"`
	// Syntax colours diff lines by token class.
	Syntax bool `json:"syntax"`
	// Stage offers the partial staging cursor in the diff pane. Turning it off
	// leaves the pane a plain reader.
	Stage bool `json:"stage"`
	// StageApply is when a keystroke reaches the index: "instant" applies each
	// decision at once, "select" marks lines and applies them together.
	StageApply string `json:"stage-apply"`
	// DiscardConfirm is the smallest thing a discard asks about: "line",
	// "hunk", "file" or "never".
	DiscardConfirm string `json:"discard-confirm"`
	// SideLabels is how the all-changes document tells the two blocks of a
	// partly staged file apart: "text", "glyph" or "off".
	SideLabels string `json:"side-labels"`
}

// The two ways a staging decision reaches the index.
const (
	// StageApplyInstant applies every keystroke as it is pressed, which is what
	// git itself does and what makes a mistake a single undo away.
	StageApplyInstant = "instant"
	// StageApplySelect marks lines and applies the marks in one go, which is
	// the shape a careful review pass wants.
	StageApplySelect = "select"
)

// How small a thing a discard asks about.
const (
	DiscardConfirmLine  = "line"
	DiscardConfirmHunk  = "hunk"
	DiscardConfirmFile  = "file"
	DiscardConfirmNever = "never"
)

// How a document names which half of a file a block shows. Only a file that is
// partly staged has two blocks, and only those have to say it.
const (
	// SideLabelsText spells the side out: "staged" / "unstaged".
	SideLabelsText = "text"
	// SideLabelsGlyph marks it with one character, for a narrower band.
	SideLabelsGlyph = "glyph"
	// SideLabelsOff leaves the band as it is.
	SideLabelsOff = "off"
)

// Default returns the built-in default configuration.
func Default() Config {
	return Config{
		Context:          3,
		IgnoreWhitespace: false,
		Wrap:             false,
		LineNumbers:      true,
		Borderless:       false,
		SidebarWidth:     32,
		ShowLeftSidebar:  false,
		ShowRightSidebar: false,
		SmoothScroll:     true,
		Watch:            true,
		Follow:           true,
		WatchDebounce:    120,
		LogLimit:         200,
		SplitView:        false,
		Syntax:           true,
		Stage:            true,
		StageApply:       StageApplyInstant,
		DiscardConfirm:   DiscardConfirmLine,
		SideLabels:       SideLabelsText,
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
	switch cfg.StageApply {
	case StageApplyInstant, StageApplySelect:
	default:
		cfg.StageApply = StageApplyInstant
	}
	switch cfg.DiscardConfirm {
	case DiscardConfirmLine, DiscardConfirmHunk, DiscardConfirmFile, DiscardConfirmNever:
	default:
		cfg.DiscardConfirm = DiscardConfirmLine
	}
	switch cfg.SideLabels {
	case SideLabelsText, SideLabelsGlyph, SideLabelsOff:
	default:
		cfg.SideLabels = SideLabelsText
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
