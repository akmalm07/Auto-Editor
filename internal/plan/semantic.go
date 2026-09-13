package plan

import (
	"auto-editor/internal/alignment"
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/asr"
	"context"
	"fmt"
	"strings"
)

type SemanticPlanner interface {
	Plan(ctx context.Context, request SemanticRequest) ([]SemanticBeat, error)
}

type SemanticRequest struct {
	Words            []TimedWord     `json:"words"`
	Assets           []SemanticAsset `json:"assets"`
	StylePreferences []string        `json:"style_preferences,omitempty"`
	AllAssets        bool            `json:"all_assets"`
}

type TimedWord struct {
	Index   int    `json:"index"`
	Text    string `json:"text"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
}
type SemanticAsset struct {
	AssetID       string   `json:"asset_id"`
	Summary       string   `json:"summary"`
	Intent        string   `json:"intent"`
	SuggestedUses []string `json:"suggested_uses,omitempty"`
}
type SemanticBeat struct {
	WordFrom  int    `json:"word_from"`
	WordTo    int    `json:"word_to"`
	AssetID   string `json:"asset_id"`
	Role      string `json:"role"`
	Rationale string `json:"rationale"`
}
type SemanticPlan struct {
	Status string         `json:"status"`
	Beats  []SemanticBeat `json:"beats,omitempty"`
}

func semanticRequest(aligned alignment.Result, assets []analyzeassets.Asset, stylePreferences []PlanningPreference, allAssets bool) SemanticRequest {
	request := SemanticRequest{Words: make([]TimedWord, 0, len(aligned.Transcript.Words)), Assets: make([]SemanticAsset, 0, len(assets)), AllAssets: allAssets}
	for index, word := range aligned.Transcript.Words {
		request.Words = append(request.Words, TimedWord{Index: index, Text: word.Text, StartMS: word.StartMS, EndMS: word.EndMS})
	}
	for _, asset := range assets {
		request.Assets = append(request.Assets, SemanticAsset{AssetID: asset.AssetID, Summary: asset.Literal.Summary, Intent: asset.Editorial.Intent, SuggestedUses: asset.Editorial.SuggestedUses})
	}
	for _, preference := range stylePreferences {
		request.StylePreferences = append(request.StylePreferences, preference.Text)
	}
	return request
}

func validateSemanticBeats(beats []SemanticBeat, words []asr.Word, assets []analyzeassets.Asset) error {
	if len(beats) == 0 || len(words) == 0 {
		return fmt.Errorf("semantic planner returned no beats")
	}
	assetIDs := map[string]bool{}
	for _, asset := range assets {
		assetIDs[asset.AssetID] = true
	}
	next := 0
	for _, beat := range beats {
		if beat.WordFrom != next || beat.WordTo < beat.WordFrom || beat.WordTo >= len(words) || !assetIDs[beat.AssetID] {
			return fmt.Errorf("beat %d-%d has invalid word coverage or asset", beat.WordFrom, beat.WordTo)
		}
		next = beat.WordTo + 1
	}
	if next != len(words) {
		return fmt.Errorf("beats do not cover every transcript word")
	}
	return nil
}

func validateAllAssets(beats []SemanticBeat, assets []analyzeassets.Asset) error {
	used := make(map[string]bool, len(beats))
	for _, beat := range beats {
		used[beat.AssetID] = true
	}
	for _, asset := range assets {
		if !used[asset.AssetID] {
			return fmt.Errorf("semantic beats did not use asset %s", asset.AssetID)
		}
	}
	return nil
}

func forcedBeats(words []asr.Word, assets []analyzeassets.Asset) ([]SemanticBeat, error) {
	if len(words) < len(assets) {
		return nil, fmt.Errorf("--all-assets requires at least one transcript word per visual asset (%d words, %d assets)", len(words), len(assets))
	}
	beats := make([]SemanticBeat, 0, len(assets))
	wordIndex := 0
	for assetIndex, asset := range assets {
		remainingWords, remainingAssets := len(words)-wordIndex, len(assets)-assetIndex
		count := (remainingWords + remainingAssets - 1) / remainingAssets
		beats = append(beats, SemanticBeat{WordFrom: wordIndex, WordTo: wordIndex + count - 1, AssetID: asset.AssetID, Role: asset.Editorial.Intent, Rationale: "--all-assets constraint"})
		wordIndex += count
	}
	return beats, nil
}

func wordsText(words []asr.Word) string {
	values := make([]string, 0, len(words))
	for _, word := range words {
		values = append(values, word.Text)
	}
	return strings.Join(values, " ")
}
