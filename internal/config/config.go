package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultConfigName = "auto-editor.config.json"

type Config struct {
	UI UIConfig `json:"ui"`
}

type UIConfig struct {
	Color    string        `json:"color"`
	Progress bool          `json:"progress"`
	Loading  LoadingConfig `json:"loading"`
}

type LoadingConfig struct {
	Enabled    bool   `json:"enabled"`
	Style      string `json:"style"`
	IntervalMS int    `json:"interval_ms"`
}

func Default() Config {
	return Config{
		UI: UIConfig{
			Color:    "auto",
			Progress: true,
			Loading: LoadingConfig{
				Enabled:    true,
				Style:      "ascii",
				IntervalMS: 80,
			},
		},
	}
}

func Read(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return Config{}, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	switch c.UI.Color {
	case "", "auto", "always", "never":
	default:
		return fmt.Errorf("invalid ui.color %q: expected auto, always, or never", c.UI.Color)
	}

	switch c.UI.Loading.Style {
	case "", "ascii", "dots", "bar":
	default:
		return fmt.Errorf("invalid ui.loading.style %q: expected ascii, dots, or bar", c.UI.Loading.Style)
	}

	if c.UI.Loading.IntervalMS < 0 {
		return fmt.Errorf("ui.loading.interval_ms must be non-negative")
	}

	return nil
}

func WriteDefault(path string) error {
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
