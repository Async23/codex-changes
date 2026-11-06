package preferences

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStateRoundTripIsPrivateAndAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "preferences.json")
	want := State{Theme: "light", Wrap: true, Split: true, LastThread: "synthetic-id", LastFile: "/synthetic/note.md"}
	want.Lists = map[string]List{
		"file":    {Sort: "natural", Reverse: true, Linemode: "changes"},
		"session": {Sort: "created", Linemode: "none"},
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, want) {
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

func TestOldPreferencesRemainReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(path, []byte(`{"theme":"dark","wrap":false,"lastThread":"synthetic-id"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := Load(path)
	if err != nil || state.Theme != "dark" || state.Wrap || state.LastThread != "synthetic-id" || len(state.Lists) != 0 {
		t.Fatalf("old preferences were not preserved: %+v %v", state, err)
	}
}
