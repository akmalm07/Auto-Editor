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
	UI        UIConfig        `json:"ui"`
	Alignment AlignmentConfig `json:"alignment"`
	Assets    AssetsConfig    `json:"assets"`
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

type AlignmentConfig struct {
	Provider          string  `json:"provider"`
	Model             string  `json:"model"`
	APIKeyEnvironment string  `json:"api_key_environment"`
	MinSimilarity     float64 `json:"min_similarity"`
}

type AssetsConfig struct {
	FFprobe           string `json:"ffprobe"`
	FFmpeg            string `json:"ffmpeg"`
	SemanticProvider  string `json:"semantic_provider"`
	SemanticModel     string `json:"semantic_model"`
	APIKeyEnvironment string `json:"api_key_environment"`
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
		Alignment: AlignmentConfig{
			Provider:          "openai",
			Model:             "whisper-1",
			APIKeyEnvironment: "OPENAI_API_KEY",
			MinSimilarity:     0.70,
		},
		Assets: AssetsConfig{
			FFprobe:           "ffprobe",
			FFmpeg:            "ffmpeg",
			SemanticProvider:  "none",
			SemanticModel:     "gpt-4.1-mini",
			APIKeyEnvironment: "OPENAI_API_KEY",
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

	switch c.Alignment.Provider {
	case "", "openai":
	default:
		return fmt.Errorf("invalid alignment.provider %q: expected openai", c.Alignment.Provider)
	}
	if c.Alignment.Model != "" && c.Alignment.Model != "whisper-1" {
		return fmt.Errorf("invalid alignment.model %q: whisper-1 is currently required for word timestamps", c.Alignment.Model)
	}
	if c.Alignment.MinSimilarity < 0 || c.Alignment.MinSimilarity > 1 {
		return fmt.Errorf("alignment.min_similarity must be between 0 and 1")
	}

	if c.Assets.FFprobe == "" {
		return fmt.Errorf("assets.ffprobe cannot be empty")
	}
	if c.Assets.FFmpeg == "" {
		return fmt.Errorf("assets.ffmpeg cannot be empty")
	}
	switch c.Assets.SemanticProvider {
	case "", "none", "openai":
	default:
		return fmt.Errorf("invalid assets.semantic_provider %q: expected none or openai", c.Assets.SemanticProvider)
	}
	if c.Assets.SemanticProvider == "openai" {
		if c.Assets.SemanticModel == "" {
			return fmt.Errorf("assets.semantic_model is required when semantic_provider is openai")
		}
		if c.Assets.APIKeyEnvironment == "" {
			return fmt.Errorf("assets.api_key_environment is required when semantic_provider is openai")
		}
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
