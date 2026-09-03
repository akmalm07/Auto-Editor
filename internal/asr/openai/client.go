package openai

import (
	"auto-editor/internal/asr"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.openai.com/v1"

type Client struct {
	APIKey  string
	Model   string
	BaseURL string
	HTTP    *http.Client
}

func (c Client) Transcribe(ctx context.Context, audioPath, language string) (asr.Transcript, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return asr.Transcript{}, fmt.Errorf("OpenAI API key is missing")
	}
	model := c.Model
	if model == "" {
		model = "whisper-1"
	}
	if model != "whisper-1" {
		return asr.Transcript{}, fmt.Errorf("model %q cannot guarantee word timestamps; use whisper-1 for alignment", model)
	}

	file, err := os.Open(audioPath)
	if err != nil {
		return asr.Transcript{}, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return asr.Transcript{}, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return asr.Transcript{}, err
	}
	_ = writer.WriteField("model", model)
	_ = writer.WriteField("response_format", "verbose_json")
	_ = writer.WriteField("timestamp_granularities[]", "word")
	_ = writer.WriteField("timestamp_granularities[]", "segment")
	if strings.TrimSpace(language) != "" {
		_ = writer.WriteField("language", language)
	}
	if err := writer.Close(); err != nil {
		return asr.Transcript{}, err
	}

	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/audio/transcriptions", &body)
	if err != nil {
		return asr.Transcript{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Minute}
	}
	response, err := httpClient.Do(req)
	if err != nil {
		return asr.Transcript{}, err
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return asr.Transcript{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return asr.Transcript{}, fmt.Errorf("OpenAI transcription failed (%s): %s", response.Status, strings.TrimSpace(string(data)))
	}

	var raw struct {
		Language string `json:"language"`
		Text     string `json:"text"`
		Words    []struct {
			Word  string  `json:"word"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"words"`
		Segments []struct {
			Text  string  `json:"text"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return asr.Transcript{}, fmt.Errorf("decode OpenAI transcription response: %w", err)
	}

	transcript := asr.Transcript{Provider: "openai", Model: model, Language: raw.Language, Text: raw.Text}
	for _, word := range raw.Words {
		transcript.Words = append(transcript.Words, asr.Word{Text: word.Word, StartMS: int64(word.Start * 1000), EndMS: int64(word.End * 1000)})
	}
	for _, segment := range raw.Segments {
		transcript.Segments = append(transcript.Segments, asr.Segment{Text: segment.Text, StartMS: int64(segment.Start * 1000), EndMS: int64(segment.End * 1000)})
	}
	if strings.TrimSpace(transcript.Text) == "" || len(transcript.Words) == 0 {
		return asr.Transcript{}, fmt.Errorf("OpenAI transcription returned no word timestamps")
	}
	return transcript, nil
}
