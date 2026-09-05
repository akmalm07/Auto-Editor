package analyzeassets

import (
	"auto-editor/internal/semanticcache"
	"context"
	"fmt"
	"path/filepath"
)

type SemanticCache = semanticcache.Cache

// LoadSemanticCache returns an empty cache when no prior analysis exists.
func LoadSemanticCache(path string) (*SemanticCache, error) {
	return semanticcache.Load(path)
}

type CachedSemanticAnalyzer struct {
	Analyzer SemanticAnalyzer
	Cache    *SemanticCache
	Scope    string
}

func (c CachedSemanticAnalyzer) Analyze(ctx context.Context, paths []string) (SemanticAnalysis, error) {
	if c.Analyzer == nil {
		return SemanticAnalysis{}, fmt.Errorf("semantic analyzer is missing")
	}
	if c.Cache == nil {
		return c.Analyzer.Analyze(ctx, paths)
	}
	key, err := semanticcache.Key(c.Scope, paths)
	if err != nil {
		return SemanticAnalysis{}, err
	}
	var analysis SemanticAnalysis
	if found, err := c.Cache.Get(key, &analysis); err == nil && found {
		return analysis, nil
	}
	analysis, err = c.Analyzer.Analyze(ctx, paths)
	if err != nil {
		return SemanticAnalysis{}, err
	}
	// The cache is an optimization. A completed provider result remains useful
	// even if the cache cannot be persisted.
	_ = c.Cache.Put(key, analysis)
	return analysis, nil
}

func DefaultSemanticCachePath(indexPath string) string {
	return filepath.Join(filepath.Dir(indexPath), "semantic-cache.json")
}
