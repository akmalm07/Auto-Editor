package openai

import (
	"auto-editor/internal/analyzestyle"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const stylePromptVersion = "style-semantic-v1"
const styleSchema = `{"type":"object","additionalProperties":false,"required":["summary","caption_conventions","humor_patterns","emotional_pacing","visual_roles","editing_techniques","planning_preferences","supporting_frame_ids","confidence"],"properties":{"summary":{"type":"string"},"caption_conventions":{"type":"array","items":{"type":"string"}},"humor_patterns":{"type":"array","items":{"type":"string"}},"emotional_pacing":{"type":"array","items":{"type":"string"}},"visual_roles":{"type":"array","items":{"type":"string"}},"editing_techniques":{"type":"array","items":{"type":"string"}},"planning_preferences":{"type":"array","items":{"type":"string"}},"supporting_frame_ids":{"type":"array","items":{"type":"string"}},"confidence":{"type":"string","enum":["low","medium","high"]}}}`

type StyleClient struct{ Client Client }

func (c StyleClient) CacheScope() string {
	return "openai/" + c.Client.Model + "/" + c.Client.imageDetail() + "/" + stylePromptVersion
}

func (c StyleClient) AnalyzeStyle(ctx context.Context, request analyzestyle.SemanticRequest) (analyzestyle.SemanticStyle, error) {
	if len(request.FramePaths) != len(request.Frames) {
		return analyzestyle.SemanticStyle{}, fmt.Errorf("style semantic frame paths do not match frame evidence")
	}
	technical, err := json.Marshal(request.Technical)
	if err != nil {
		return analyzestyle.SemanticStyle{}, err
	}
	labels := make([]string, 0, len(request.Frames))
	for _, frame := range request.Frames {
		labels = append(labels, frame.ID)
	}
	prompt := "Analyze this named reference-video set as a whole. Use the technical measurements as evidence, but infer only recurring creative conventions that are supported by the supplied frames. Describe captions, humor, emotional pacing, visual roles, editing techniques, and preferences for a future edit plan. Do not copy individual videos or invent unsupported patterns. Frame IDs, in image order: [" + strings.Join(labels, ", ") + "]. Technical profile: " + string(technical)
	var semantic analyzestyle.SemanticStyle
	err = c.Client.AnalyzeJSON(ctx, VisionRequest{Prompt: prompt, SchemaName: "reference_style_analysis", Schema: json.RawMessage(styleSchema), Paths: request.FramePaths}, &semantic)
	if err != nil {
		return analyzestyle.SemanticStyle{}, fmt.Errorf("OpenAI style semantic analysis: %w", err)
	}
	return semantic, nil
}
