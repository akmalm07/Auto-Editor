// Package semanticcache persists successful provider results for unchanged visual inputs.
package semanticcache

import (
	"auto-editor/internal/artifact"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

const Version = 1

type Cache struct {
	path    string
	entries map[string]json.RawMessage
	mu      sync.Mutex
}

type file struct {
	Version int                        `json:"version"`
	Entries map[string]json.RawMessage `json:"entries"`
}

func Load(path string) (*Cache, error) {
	cache := &Cache{path: path, entries: make(map[string]json.RawMessage)}
	data, err := artifact.Read[file](path)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read semantic cache: %w", err)
	}
	if data.Version == Version && data.Entries != nil {
		cache.entries = data.Entries
	}
	return cache, nil
}

func (c *Cache) Get(key string, value any) (bool, error) {
	c.mu.Lock()
	data, ok := c.entries[key]
	c.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(data, value)
}

func (c *Cache) Put(key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = data
	return artifact.Write(c.path, file{Version: Version, Entries: c.entries})
}

func Key(scope string, paths []string) (string, error) {
	hash := sha256.New()
	_, _ = hash.Write([]byte(scope + "\n"))
	for index, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		_, _ = fmt.Fprintf(hash, "%d:%s\n", index, hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
