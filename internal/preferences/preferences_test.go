package preferences

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTripIsPrivateAndAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "preferences.json")
	want := State{Theme: "light", Wrap: true, Split: true, LastThread: "synthetic-id", LastFile: "/synthetic/note.md"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != want {
		t.Fatalf("got %+v %v", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("insecure mode %v", info.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("left a temporary file")
	}
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("corrupted state was not reported")
	}
}
