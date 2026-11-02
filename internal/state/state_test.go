package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "bookmarks.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.SetLast("jsd@nas", "/home/jsd/Music", "/srv/media")
	s.AddBookmark("jsd@nas", Bookmark{Name: "photos", Local: "/home/jsd/Pictures", Remote: "/srv/photos"})
	s.AddBookmark("jsd@nas", Bookmark{Name: "Backups", Local: "/home/jsd", Remote: "/backup"})
	s.AddBookmark("jsd@nas", Bookmark{Name: "photos", Local: "/home/jsd/Pictures/2026", Remote: "/srv/photos/2026"}) // replaces
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm()&0o077 != 0 {
		t.Errorf("bookmarks file should be private: %v %v", st.Mode(), err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	last, ok := got.Last("jsd@nas")
	if !ok || last.Local != "/home/jsd/Music" || last.Remote != "/srv/media" {
		t.Errorf("Last = %+v, %v", last, ok)
	}
	bms := got.Bookmarks("jsd@nas")
	if len(bms) != 2 || bms[0].Name != "Backups" || bms[1].Remote != "/srv/photos/2026" {
		t.Errorf("Bookmarks = %+v", bms)
	}
	got.RemoveBookmark("jsd@nas", "Backups")
	if len(got.Bookmarks("jsd@nas")) != 1 {
		t.Error("RemoveBookmark did nothing")
	}
	if _, ok := got.Last("someone@else"); ok || got.Bookmarks("someone@else") != nil {
		t.Error("unknown host should have nothing")
	}
}

func TestCorruptFileStartsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bookmarks.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for a corrupt file")
	}
	if s == nil || len(s.Hosts) != 0 {
		t.Fatal("expected a usable empty store")
	}
	s.SetLast("a@b", "/x", "/y")
	if err := s.Save(); err != nil {
		t.Fatalf("saving after a corrupt load: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("file still unreadable after save: %v", err)
	}
}

func TestSetLastIgnoresEmpty(t *testing.T) {
	var s Store
	s.SetLast("k", "", "/r")
	if _, ok := s.Last("k"); ok {
		t.Error("an empty side should not be recorded")
	}
}
