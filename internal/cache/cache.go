// Package cache is a tiny file based cache with per-entry TTL.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Forever can be used as TTL for immutable data.
const Forever = 100 * 365 * 24 * time.Hour

type Cache struct {
	dir      string
	disabled bool // no reads (writes still happen, so --refresh updates the cache)
}

type entry struct {
	Key     string          `json:"key"`
	Stored  time.Time       `json:"stored"`
	Expires time.Time       `json:"expires"`
	Data    json.RawMessage `json:"data"`
}

// New creates a cache rooted at dir. If bypass is true, Get always misses.
func New(dir string, bypass bool) *Cache {
	return &Cache{dir: dir, disabled: bypass}
}

func (c *Cache) path(key string) string {
	h := sha256.Sum256([]byte(key))
	s := hex.EncodeToString(h[:])
	return filepath.Join(c.dir, s[:2], s+".json")
}

// Get returns the cached bytes for key if present and not expired.
func (c *Cache) Get(key string) ([]byte, bool) {
	if c == nil || c.disabled {
		return nil, false
	}
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var e entry
	if json.Unmarshal(b, &e) != nil || e.Key != key || time.Now().After(e.Expires) {
		return nil, false
	}
	return e.Data, true
}

// Put stores data (must be valid JSON) under key.
func (c *Cache) Put(key string, data []byte, ttl time.Duration) {
	if c == nil || ttl <= 0 || !json.Valid(data) {
		return
	}
	now := time.Now()
	b, err := json.Marshal(entry{Key: key, Stored: now, Expires: now.Add(ttl), Data: data})
	if err != nil {
		return
	}
	p := c.path(key)
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o600)
}

// Clear removes all cached entries.
func (c *Cache) Clear() error {
	return os.RemoveAll(c.dir)
}
