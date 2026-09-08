package openai

import (
	"auto-editor/internal/ai"
	"auto-editor/internal/analyzeassets"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.openai.com/v1"
const semanticPromptVersion = "asset-semantic-v2"
const semanticPrompt = "Analyze every supplied frame as one visual asset. literal must contain only visible evidence. editorial_meaning should identify the likely reaction, joke, emotional beat, and useful edit roles across the full frame sequence. Transcribe meaningful on-screen text. Be concise; do not invent facts."

const semanticSchema = `{"type":"object","additionalProperties":false,"required":["literal","editorial_meaning"],"properties":{"literal":{"type":"object","additionalProperties":false,"required":["summary","subjects","actions","settings","mood","visible_text"],"properties":{"summary":{"type":"string"},"subjects":{"type":"array","items":{"type":"string"}},"actions":{"type":"array","items":{"type":"string"}},"settings":{"type":"array","items":{"type":"string"}},"mood":{"type":"array","items":{"type":"string"}},"visible_text":{"type":"array","items":{"type":"string"}}}},"editorial_meaning":{"type":"object","additionalProperties":false,"required":["summary","intent","emotional_beat","suggested_uses"],"properties":{"summary":{"type":"string"},"intent":{"type":"string"},"emotional_beat":{"type":"string"},"suggested_uses":{"type":"array","items":{"type":"string"}}}}}}`

type Client struct {
	APIKey, Model, BaseURL string
	HTTP                   *http.Client
	ImageDetail            string
	MaxAttempts            int
	Timeout                time.Duration
}

type VisionRequest struct {
	Prompt     string
	SchemaName string
	Schema     json.RawMessage
	Paths      []string
}

func (c Client) CacheScope() string {
	return "openai/" + c.Model + "/" + c.imageDetail() + "/" + semanticPromptVersion
}

func (c Client) Analyze(ctx context.Context, paths []string) (analyzeassets.SemanticAnalysis, error) {
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
	if err := c.AnalyzeJSON(ctx, VisionRequest{Prompt: semanticPrompt, SchemaName: "asset_semantic_analysis", Schema: json.RawMessage(semanticSchema), Paths: paths}, &rawResult); err != nil {
		return analyzeassets.SemanticAnalysis{}, fmt.Errorf("OpenAI semantic analysis: %w", err)
	}
	return analyzeassets.SemanticAnalysis{
		Literal:   analyzeassets.LiteralContent{Summary: rawResult.Literal.Summary, Subjects: rawResult.Literal.Subjects, Actions: rawResult.Literal.Actions, Settings: rawResult.Literal.Settings, Mood: rawResult.Literal.Mood, VisibleText: rawResult.Literal.VisibleText},
		Editorial: analyzeassets.EditorialMeaning{Summary: rawResult.Editorial.Summary, Intent: rawResult.Editorial.Intent, EmotionalBeat: rawResult.Editorial.EmotionalBeat, SuggestedUses: rawResult.Editorial.SuggestedUses},
	}, nil
}

// AnalyzeJSON sends one bounded multi-image Responses API request and decodes
// the schema-constrained output into target.
func (c Client) AnalyzeJSON(ctx context.Context, input VisionRequest, target any) error {
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("OpenAI API key is missing")
	}
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("OpenAI semantic model is missing")
	}
	imageURLs := make([]string, 0, len(input.Paths))
	for _, path := range input.Paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		imageURLs = append(imageURLs, "data:"+mediaType(path)+";base64,"+base64.StdEncoding.EncodeToString(data))
	}
	detail := c.imageDetail()
	statusCode, status, responseBody, err := c.requestJSON(ctx, input, imageURLs, detail)
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("OpenAI semantic analysis failed (%s): %s", status, providerError(responseBody))
	}
	return decodeResponse(responseBody, target)
}

// requestJSON makes one bounded Responses API request at the requested
// image-detail level.
func (c Client) requestJSON(ctx context.Context, input VisionRequest, imageURLs []string, detail string) (int, string, []byte, error) {
	content := []map[string]string{{"type": "input_text", "text": input.Prompt}}
	for _, imageURL := range imageURLs {
		content = append(content, map[string]string{"type": "input_image", "image_url": imageURL, "detail": detail})
	}
	body, err := json.Marshal(map[string]any{
		"model": c.Model,
		"store": false,
		"input": []map[string]any{{"role": "user", "content": content}},
		"text": map[string]any{
			"verbosity": "medium",
			"format":    map[string]any{"type": "json_schema", "name": input.SchemaName, "strict": true, "schema": input.Schema},
		},
	})
	if err != nil {
		return 0, "", nil, err
	}
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return 0, "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := ai.Do(ctx, c.httpClient(), req, ai.RetryOptions{MaxAttempts: c.MaxAttempts})
	if err != nil {
		return 0, "", nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return 0, "", nil, err
	}
	return response.StatusCode, response.Status, responseBody, nil
}

func decodeResponse(responseBody []byte, target any) error {
	var raw struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(responseBody, &raw); err != nil {
		return err
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
	if err := json.Unmarshal([]byte(text), target); err != nil {
		return fmt.Errorf("decode OpenAI semantic response: %w", err)
	}
	return nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	return &http.Client{Timeout: timeout}
}

func (c Client) imageDetail() string {
	if c.ImageDetail == "low" || c.ImageDetail == "medium" || c.ImageDetail == "high" || c.ImageDetail == "auto" || c.ImageDetail == "original" {
		return c.ImageDetail
	}
	return "auto"
}

func mediaType(path string) string {
	if value := mime.TypeByExtension(filepath.Ext(path)); value != "" {
		return value
	}
	return "image/jpeg"
}

func providerError(body []byte) string {
	var response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &response) == nil && strings.TrimSpace(response.Error.Message) != "" {
		return response.Error.Message
	}
	return strings.TrimSpace(string(body))
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
