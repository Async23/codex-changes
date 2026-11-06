package preferences

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Only UI preferences and the last selection are saved; never history bodies.
type State struct {
	Theme      string          `json:"theme"`
	Wrap       bool            `json:"wrap"`
	Split      bool            `json:"split"`
	LastThread string          `json:"lastThread,omitempty"`
	LastFile   string          `json:"lastFile,omitempty"`
	Lists      map[string]List `json:"lists,omitempty"`
}

// List stores independent choices for each navigation level.
type List struct {
	Sort     string `json:"sort,omitempty"`
	Reverse  bool   `json:"reverse,omitempty"`
	Linemode string `json:"linemode,omitempty"`
}

func Defaults() State { return State{Theme: "auto", Wrap: true} }

func Path() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "codex-changes", "preferences.json"), nil
}

func Load(path string) (State, error) {
	state := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return Defaults(), err
	}
	switch state.Theme {
	case "auto", "dark", "light":
	default:
		state.Theme = "auto"
	}
	return state, nil
}

func Save(path string, state State) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".preferences-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
