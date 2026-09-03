package openai

import (
	"auto-editor/internal/analyzeassets"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.openai.com/v1"

type Client struct {
	APIKey, Model, BaseURL string
	HTTP                   *http.Client
}

func (c Client) Analyze(ctx context.Context, paths []string) (analyzeassets.SemanticAnalysis, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return analyzeassets.SemanticAnalysis{}, fmt.Errorf("OpenAI API key is missing")
	}
	if strings.TrimSpace(c.Model) == "" {
		return analyzeassets.SemanticAnalysis{}, fmt.Errorf("OpenAI semantic model is missing")
	}
	if len(paths) == 0 {
		return analyzeassets.SemanticAnalysis{}, fmt.Errorf("semantic analysis requires an image")
	}
	content := []map[string]string{{"type": "input_text", "text": "Analyze these visual asset frames. Return only valid JSON with two objects: literal (summary, subjects, actions, settings, mood, visible_text) and editorial_meaning (summary, intent, emotional_beat, suggested_uses). Literal content must describe only visible evidence. Editorial meaning should infer the likely joke, reaction, emotional beat, and editing use from the full set of frames, but stay concise and avoid unsupported claims."}}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return analyzeassets.SemanticAnalysis{}, err
		}
		content = append(content, map[string]string{"type": "input_image", "image_url": "data:" + mime.TypeByExtension(filepath.Ext(path)) + ";base64," + base64.StdEncoding.EncodeToString(data)})
	}
	body, err := json.Marshal(map[string]any{"model": c.Model, "store": false, "input": []map[string]any{{"role": "user", "content": content}}})
	if err != nil {
		return analyzeassets.SemanticAnalysis{}, err
	}
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return analyzeassets.SemanticAnalysis{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	response, err := client.Do(req)
	if err != nil {
		return analyzeassets.SemanticAnalysis{}, err
	}
	defer response.Body.Close()
	var raw struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(response.Body).Decode(&raw); err != nil {
		return analyzeassets.SemanticAnalysis{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return analyzeassets.SemanticAnalysis{}, fmt.Errorf("OpenAI semantic analysis failed (%s)", response.Status)
	}
	text := raw.OutputText
	if text == "" {
		for _, item := range raw.Output {
			for _, part := range item.Content {
				if part.Type == "output_text" || part.Type == "text" {
					text += part.Text
				}
			}
		}
	}
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	var rawResult struct {
		Literal struct {
			Summary     string     `json:"summary"`
			Subjects    stringList `json:"subjects"`
			Actions     stringList `json:"actions"`
			Settings    stringList `json:"settings"`
			Mood        stringList `json:"mood"`
			VisibleText stringList `json:"visible_text"`
		} `json:"literal"`
		Editorial struct {
			Summary       string     `json:"summary"`
			Intent        string     `json:"intent"`
			EmotionalBeat string     `json:"emotional_beat"`
			SuggestedUses stringList `json:"suggested_uses"`
		} `json:"editorial_meaning"`
	}
	if err := json.Unmarshal([]byte(text), &rawResult); err != nil {
		return analyzeassets.SemanticAnalysis{}, fmt.Errorf("decode OpenAI semantic response: %w", err)
	}
	return analyzeassets.SemanticAnalysis{
		Literal:   analyzeassets.LiteralContent{Summary: rawResult.Literal.Summary, Subjects: rawResult.Literal.Subjects, Actions: rawResult.Literal.Actions, Settings: rawResult.Literal.Settings, Mood: rawResult.Literal.Mood, VisibleText: rawResult.Literal.VisibleText},
		Editorial: analyzeassets.EditorialMeaning{Summary: rawResult.Editorial.Summary, Intent: rawResult.Editorial.Intent, EmotionalBeat: rawResult.Editorial.EmotionalBeat, SuggestedUses: rawResult.Editorial.SuggestedUses},
	}, nil
}

type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		*s = values
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		*s = nil
		return nil
	}
	*s = []string{value}
	return nil
}
