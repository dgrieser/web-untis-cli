// Package tracker persists sets of already handled item ids (forwarded
// messages, seen news, …) in small JSON files inside the profile directory.
package tracker

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dgrieser/web-untis-cli/internal/config"
)

// Set is a persisted set of keys ("<kind>/<id>") with the time they were added.
type Set struct {
	path  string
	Items map[string]time.Time `json:"forwarded"` // name kept for compatibility with existing files
}

// Load reads a set from path (empty set if the file does not exist).
func Load(path string) (*Set, error) {
	s := &Set{path: path, Items: map[string]time.Time{}}
	err := config.ReadJSON(path, s)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if s.Items == nil {
		s.Items = map[string]time.Time{}
	}
	return s, nil
}

// Key builds the key of an item.
func Key(kind string, id int) string { return fmt.Sprintf("%s/%d", kind, id) }

// Has reports whether an item is in the set.
func (s *Set) Has(kind string, id int) bool {
	_, ok := s.Items[Key(kind, id)]
	return ok
}

// Since returns when an item was added (zero if unknown).
func (s *Set) Since(kind string, id int) time.Time { return s.Items[Key(kind, id)] }

// Mark adds an item (keeps the original time if already present).
func (s *Set) Mark(kind string, id int) {
	if !s.Has(kind, id) {
		s.Items[Key(kind, id)] = time.Now()
	}
}

// Save writes the set.
func (s *Set) Save() error { return config.WriteJSON(s.path, s) }
