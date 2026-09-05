package analyzestyle

import (
	"auto-editor/internal/semanticcache"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

type CachedSemanticAnalyzer struct {
	Analyzer SemanticAnalyzer
	Cache    *semanticcache.Cache
	Scope    string
}

func (c CachedSemanticAnalyzer) AnalyzeStyle(ctx context.Context, request SemanticRequest) (SemanticStyle, error) {
	if c.Analyzer == nil {
		return SemanticStyle{}, fmt.Errorf("semantic style analyzer is missing")
	}
	if c.Cache == nil {
		return c.Analyzer.AnalyzeStyle(ctx, request)
	}
	technical, err := json.Marshal(request.Technical)
	if err != nil {
		return SemanticStyle{}, err
	}
	key, err := semanticcache.Key(c.Scope+"/technical="+string(technical), request.FramePaths)
	if err != nil {
		return SemanticStyle{}, err
	}
	var semantic SemanticStyle
	if found, err := c.Cache.Get(key, &semantic); err == nil && found {
		return semantic, nil
	}
	semantic, err = c.Analyzer.AnalyzeStyle(ctx, request)
	if err != nil {
		return SemanticStyle{}, err
	}
	_ = c.Cache.Put(key, semantic)
	return semantic, nil
}

func DefaultSemanticCachePath(indexPath string) string {
	return filepath.Join(filepath.Dir(indexPath), "style-semantic-cache.json")
}

func LoadSemanticCache(path string) (*semanticcache.Cache, error) {
	return semanticcache.Load(path)
}
