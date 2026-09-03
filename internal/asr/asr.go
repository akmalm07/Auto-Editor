package asr

import "context"

type Word struct {
	Text    string  `json:"text"`
	StartMS int64   `json:"start_ms"`
	EndMS   int64   `json:"end_ms"`
	Score   float64 `json:"score,omitempty"`
}

type Segment struct {
	Text    string `json:"text"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
}

type Transcript struct {
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Language string    `json:"language,omitempty"`
	Text     string    `json:"text"`
	Words    []Word    `json:"words,omitempty"`
	Segments []Segment `json:"segments,omitempty"`
}

type Transcriber interface {
	Transcribe(ctx context.Context, audioPath, language string) (Transcript, error)
}
