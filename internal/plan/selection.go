package plan

import (
	"auto-editor/internal/analyzeassets"
	"auto-editor/internal/analyzestyle"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const selectionPolicy = "style-semantic-soft-ranking-v1"

type AssetSelection struct {
	Policy      string               `json:"policy"`
	StyleStatus string               `json:"style_status"`
	Preferences []PlanningPreference `json:"preferences,omitempty"`
	Ranking     []AssetRanking       `json:"ranking"`
}

type PlanningPreference struct {
	ID           string `json:"id"`
	ReferenceSet string `json:"reference_set"`
	Text         string `json:"text"`
	Confidence   string `json:"confidence"`
	Weight       int    `json:"weight"`
}

type AssetRanking struct {
	AssetID    string            `json:"asset_id"`
	BaseOrder  int               `json:"base_order"`
	StyleScore int               `json:"style_score"`
	Matches    []PreferenceMatch `json:"matches,omitempty"`
}

type PreferenceMatch struct {
	PreferenceID string   `json:"preference_id"`
	MatchedTerms []string `json:"matched_terms"`
	Contribution int      `json:"contribution"`
}

type rankedAsset struct {
	asset   analyzeassets.Asset
	ranking AssetRanking
}

func rankVisualAssets(assets []analyzeassets.Asset, style analyzestyle.Result, styleAvailable bool) ([]analyzeassets.Asset, AssetSelection) {
	selection := AssetSelection{Policy: selectionPolicy, StyleStatus: "unavailable"}
	if styleAvailable {
		selection.StyleStatus = "no_preferences"
		selection.Preferences = planningPreferences(style)
	}

	ranked := make([]rankedAsset, 0, len(assets))
	matched := false
	for index, asset := range assets {
		ranking := AssetRanking{AssetID: asset.AssetID, BaseOrder: index}
		terms := assetSemanticTerms(asset)
		for _, preference := range selection.Preferences {
			common := intersectTerms(preferenceTerms(preference.Text), terms)
			if len(common) == 0 {
				continue
			}
			matched = true
			ranking.StyleScore += preference.Weight
			ranking.Matches = append(ranking.Matches, PreferenceMatch{PreferenceID: preference.ID, MatchedTerms: common, Contribution: preference.Weight})
		}
		ranked = append(ranked, rankedAsset{asset: asset, ranking: ranking})
	}

	if len(selection.Preferences) > 0 {
		selection.StyleStatus = "no_matches"
		if matched {
			selection.StyleStatus = "applied"
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].ranking.StyleScore > ranked[j].ranking.StyleScore
	})

	ordered := make([]analyzeassets.Asset, 0, len(ranked))
	selection.Ranking = make([]AssetRanking, 0, len(ranked))
	for _, candidate := range ranked {
		ordered = append(ordered, candidate.asset)
		selection.Ranking = append(selection.Ranking, candidate.ranking)
	}
	return ordered, selection
}

func planningPreferences(style analyzestyle.Result) []PlanningPreference {
	var preferences []PlanningPreference
	for _, set := range style.ReferenceSets {
		if set.Semantic.Status != "complete" {
			continue
		}
		for index, text := range set.Semantic.PlanningPreferences {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			preferences = append(preferences, PlanningPreference{
				ID:           fmt.Sprintf("%s/%d", set.Name, index),
				ReferenceSet: set.Name,
				Text:         text,
				Confidence:   set.Semantic.Confidence,
				Weight:       confidenceWeight(set.Semantic.Confidence),
			})
		}
	}
	return preferences
}

func confidenceWeight(confidence string) int {
	switch strings.ToLower(strings.TrimSpace(confidence)) {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func assetSemanticTerms(asset analyzeassets.Asset) map[string]bool {
	values := []string{asset.Kind}
	if asset.Literal.Status == "complete" {
		values = append(values, asset.Literal.Summary)
		values = append(values, asset.Literal.Subjects...)
		values = append(values, asset.Literal.Actions...)
		values = append(values, asset.Literal.Settings...)
		values = append(values, asset.Literal.Mood...)
		values = append(values, asset.Literal.VisibleText...)
	}
	if asset.Editorial.Status == "complete" {
		values = append(values, asset.Editorial.Summary, asset.Editorial.Intent, asset.Editorial.EmotionalBeat)
		values = append(values, asset.Editorial.SuggestedUses...)
	}
	return normalizedTerms(strings.Join(values, " "))
}

func preferenceTerms(value string) map[string]bool {
	return normalizedTerms(value)
}

func normalizedTerms(value string) map[string]bool {
	terms := make(map[string]bool)
	for _, raw := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		term := canonicalTerm(raw)
		if len([]rune(term)) < 3 || ignoredTerms[term] {
			continue
		}
		terms[term] = true
	}
	return terms
}

func canonicalTerm(value string) string {
	if len(value) > 4 && strings.HasSuffix(value, "s") && !strings.HasSuffix(value, "ss") {
		return strings.TrimSuffix(value, "s")
	}
	return value
}

func intersectTerms(left, right map[string]bool) []string {
	var common []string
	for term := range left {
		if right[term] {
			common = append(common, term)
		}
	}
	sort.Strings(common)
	return common
}

var ignoredTerms = map[string]bool{
	"and": true, "are": true, "asset": true, "clip": true, "edit": true,
	"editing": true, "favor": true, "for": true, "from": true, "into": true,
	"plan": true, "preference": true, "prefer": true, "should": true, "that": true,
	"the": true, "their": true, "these": true, "this": true, "use": true,
	"using": true, "visual": true, "with": true,
}
