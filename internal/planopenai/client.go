package planopenai

import (
	assetopenai "auto-editor/internal/assetvision/openai"
	"auto-editor/internal/plan"
	"context"
	"encoding/json"
	"fmt"
)

const schema = `{"type":"object","additionalProperties":false,"required":["beats"],"properties":{"beats":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["word_from","word_to","asset_id","role","rationale"],"properties":{"word_from":{"type":"integer","minimum":0},"word_to":{"type":"integer","minimum":0},"asset_id":{"type":"string"},"role":{"type":"string"},"rationale":{"type":"string"}}}}}}`

type Client struct{ Client assetopenai.Client }

func (c Client) Plan(ctx context.Context, request plan.SemanticRequest) ([]plan.SemanticBeat, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	prompt := "Create a constrained visual edit plan. Split the supplied timed transcript words into 2-6 coherent visual beats when the narration changes topic, scene, action, emotion, or explicitly signals a transition. Every transcript word must be covered exactly once by contiguous beats. Select only an asset_id from the supplied assets. Use the asset metadata and optional style preferences to match each beat's meaning. Do not invent assets, timestamps, or facts. Return concise roles and rationales. Input: " + string(data)
	var response struct {
		Beats []plan.SemanticBeat `json:"beats"`
	}
	if err := c.Client.AnalyzeJSON(ctx, assetopenai.VisionRequest{Prompt: prompt, SchemaName: "semantic_edit_plan", Schema: json.RawMessage(schema)}, &response); err != nil {
		return nil, fmt.Errorf("OpenAI semantic planning: %w", err)
	}
	return response.Beats, nil
}
