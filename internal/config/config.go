package config

import (
	"auto-editor/internal/artifact"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const DefaultConfigName = "auto-editor.config.json"

type Config struct {
	UI        UIConfig        `json:"ui"`
	Alignment AlignmentConfig `json:"alignment"`
	Assets    AssetsConfig    `json:"assets"`
	Style     StyleConfig     `json:"style"`
	Planning  PlanningConfig  `json:"planning"`
	Render    RenderConfig    `json:"render"`
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
	FFprobe                string `json:"ffprobe"`
	FFmpeg                 string `json:"ffmpeg"`
	SemanticProvider       string `json:"semantic_provider"`
	SemanticModel          string `json:"semantic_model"`
	APIKeyEnvironment      string `json:"api_key_environment"`
	SemanticCache          bool   `json:"semantic_cache"`
	SemanticImageDetail    string `json:"semantic_image_detail"`
	SemanticMaxAttempts    int    `json:"semantic_max_attempts"`
	SemanticTimeoutSeconds int    `json:"semantic_timeout_seconds"`
}

type StyleConfig struct {
	SemanticProvider       string `json:"semantic_provider"`
	SemanticModel          string `json:"semantic_model"`
	APIKeyEnvironment      string `json:"api_key_environment"`
	SemanticCache          bool   `json:"semantic_cache"`
	SemanticImageDetail    string `json:"semantic_image_detail"`
	SemanticMaxFrames      int    `json:"semantic_max_frames"`
	SemanticMaxAttempts    int    `json:"semantic_max_attempts"`
	SemanticTimeoutSeconds int    `json:"semantic_timeout_seconds"`
}

type RenderConfig struct {
	FFmpeg         string        `json:"ffmpeg"`
	FFprobe        string        `json:"ffprobe"`
	Width          int           `json:"width"`
	Height         int           `json:"height"`
	FPS            int           `json:"fps"`
	VideoCodec     string        `json:"video_codec"`
	CRF            int           `json:"crf"`
	Preset         string        `json:"preset"`
	AudioCodec     string        `json:"audio_codec"`
	AudioBitrate   string        `json:"audio_bitrate"`
	MusicDucking   bool          `json:"music_ducking"`
	MusicDuckRatio float64       `json:"music_duck_ratio"`
	MusicVolumeDB  float64       `json:"music_volume_db"`
	MusicFadeInMS  int64         `json:"music_fade_in_ms"`
	MusicFadeOutMS int64         `json:"music_fade_out_ms"`
	Captions       CaptionConfig `json:"captions"`
}

type CaptionConfig struct {
	Enabled      bool   `json:"enabled"`
	FontFamily   string `json:"font_family"`
	FontSize     int    `json:"font_size"`
	MarginBottom int    `json:"margin_bottom"`
	OutlineWidth int    `json:"outline_width"`
}

type PlanningConfig struct {
	SemanticProvider       string `json:"semantic_provider"`
	SemanticModel          string `json:"semantic_model"`
	APIKeyEnvironment      string `json:"api_key_environment"`
	SemanticMaxAttempts    int    `json:"semantic_max_attempts"`
	SemanticTimeoutSeconds int    `json:"semantic_timeout_seconds"`
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
			FFprobe:                "ffprobe",
			FFmpeg:                 "ffmpeg",
			SemanticProvider:       "openai",
			SemanticModel:          "gpt-4o-mini",
			APIKeyEnvironment:      "OPENAI_API_KEY",
			SemanticCache:          true,
			SemanticImageDetail:    "auto",
			SemanticMaxAttempts:    3,
			SemanticTimeoutSeconds: 60,
		},
		Style: StyleConfig{
			SemanticProvider:       "openai",
			SemanticModel:          "gpt-4o-mini",
			APIKeyEnvironment:      "OPENAI_API_KEY",
			SemanticCache:          true,
			SemanticImageDetail:    "auto",
			SemanticMaxFrames:      12,
			SemanticMaxAttempts:    3,
			SemanticTimeoutSeconds: 60,
		},
		Planning: PlanningConfig{
			SemanticProvider: "openai", SemanticModel: "gpt-4o-mini", APIKeyEnvironment: "OPENAI_API_KEY", SemanticMaxAttempts: 3, SemanticTimeoutSeconds: 60,
		},
		Render: RenderConfig{
			FFmpeg: "ffmpeg", FFprobe: "ffprobe", Width: 1920, Height: 1080, FPS: 30,
			VideoCodec: "libx264", CRF: 18, Preset: "medium", AudioCodec: "aac", AudioBitrate: "192k",
			MusicDucking: true, MusicDuckRatio: 8, MusicVolumeDB: -18, MusicFadeInMS: 500, MusicFadeOutMS: 500,
			Captions: CaptionConfig{Enabled: true, FontFamily: "Arial", FontSize: 56, MarginBottom: 140, OutlineWidth: 4},
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
	switch c.Assets.SemanticImageDetail {
	case "", "auto", "low", "medium", "high", "original":
	default:
		return fmt.Errorf("invalid assets.semantic_image_detail %q: expected auto, low, medium, high, or original", c.Assets.SemanticImageDetail)
	}
	if c.Assets.SemanticMaxAttempts < 1 || c.Assets.SemanticMaxAttempts > 5 {
		return fmt.Errorf("assets.semantic_max_attempts must be between 1 and 5")
	}
	if c.Assets.SemanticTimeoutSeconds < 1 || c.Assets.SemanticTimeoutSeconds > 300 {
		return fmt.Errorf("assets.semantic_timeout_seconds must be between 1 and 300")
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

	switch c.Style.SemanticProvider {
	case "", "none", "openai":
	default:
		return fmt.Errorf("invalid style.semantic_provider %q: expected none or openai", c.Style.SemanticProvider)
	}
	switch c.Style.SemanticImageDetail {
	case "", "auto", "low", "medium", "high", "original":
	default:
		return fmt.Errorf("invalid style.semantic_image_detail %q: expected auto, low, medium, high, or original", c.Style.SemanticImageDetail)
	}
	if c.Style.SemanticMaxFrames < 1 || c.Style.SemanticMaxFrames > 24 {
		return fmt.Errorf("style.semantic_max_frames must be between 1 and 24")
	}
	if c.Style.SemanticMaxAttempts < 1 || c.Style.SemanticMaxAttempts > 5 {
		return fmt.Errorf("style.semantic_max_attempts must be between 1 and 5")
	}
	if c.Style.SemanticTimeoutSeconds < 1 || c.Style.SemanticTimeoutSeconds > 300 {
		return fmt.Errorf("style.semantic_timeout_seconds must be between 1 and 300")
	}
	if c.Style.SemanticProvider == "openai" {
		if c.Style.SemanticModel == "" {
			return fmt.Errorf("style.semantic_model is required when semantic_provider is openai")
		}
		if c.Style.APIKeyEnvironment == "" {
			return fmt.Errorf("style.api_key_environment is required when semantic_provider is openai")
		}
	}

	switch c.Planning.SemanticProvider {
	case "", "none", "openai":
	default:
		return fmt.Errorf("invalid planning.semantic_provider %q: expected none or openai", c.Planning.SemanticProvider)
	}
	if c.Planning.SemanticMaxAttempts < 1 || c.Planning.SemanticMaxAttempts > 5 {
		return fmt.Errorf("planning.semantic_max_attempts must be between 1 and 5")
	}
	if c.Planning.SemanticTimeoutSeconds < 1 || c.Planning.SemanticTimeoutSeconds > 300 {
		return fmt.Errorf("planning.semantic_timeout_seconds must be between 1 and 300")
	}
	if c.Planning.SemanticProvider == "openai" && (c.Planning.SemanticModel == "" || c.Planning.APIKeyEnvironment == "") {
		return fmt.Errorf("planning semantic model and API key environment are required when semantic_provider is openai")
	}

	if c.Render.FFmpeg == "" || c.Render.FFprobe == "" {
		return fmt.Errorf("render.ffmpeg and render.ffprobe cannot be empty")
	}
	if c.Render.Width <= 0 || c.Render.Height <= 0 || c.Render.FPS <= 0 {
		return fmt.Errorf("render.width, render.height, and render.fps must be positive")
	}
	if c.Render.VideoCodec == "" || c.Render.AudioCodec == "" || c.Render.AudioBitrate == "" {
		return fmt.Errorf("render video and audio codec settings cannot be empty")
	}
	if c.Render.CRF < 0 || c.Render.CRF > 51 {
		return fmt.Errorf("render.crf must be between 0 and 51")
	}
	if c.Render.MusicDuckRatio < 1 || c.Render.MusicDuckRatio > 20 {
		return fmt.Errorf("render.music_duck_ratio must be between 1 and 20")
	}
	if c.Render.MusicVolumeDB < -60 || c.Render.MusicVolumeDB > 12 {
		return fmt.Errorf("render.music_volume_db must be between -60 and 12")
	}
	if c.Render.MusicFadeInMS < 0 || c.Render.MusicFadeOutMS < 0 {
		return fmt.Errorf("render music fade durations must be non-negative")
	}
	if c.Render.Captions.Enabled && (c.Render.Captions.FontFamily == "" || c.Render.Captions.FontSize <= 0 || c.Render.Captions.MarginBottom < 0 || c.Render.Captions.OutlineWidth < 0) {
		return fmt.Errorf("render caption settings are invalid")
	}

	return nil
}

func WriteDefault(path string) error {
	return artifact.Write(path, Default())
}
