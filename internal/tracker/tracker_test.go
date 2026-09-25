package tracker

import (
	"path/filepath"
	"testing"
)

func TestSet(t *testing.T) {
	p := filepath.Join(t.TempDir(), "set.json")
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	s.Mark("inbox", 1)
	first := s.Since("inbox", 1)
	s.Mark("inbox", 1)
	if !s.Since("inbox", 1).Equal(first) {
		t.Fatal("Mark must keep the first time")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, _ := Load(p)
	if !s2.Has("inbox", 1) || s2.Has("inbox", 2) || s2.Has("sent", 1) {
		t.Fatalf("set: %+v", s2.Items)
	}
}
