package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"trade_bot/internal/healthwatch"
)

func TestStateIsPersistedPrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := healthwatch.State{Incident: true, NotifiedIncident: true, Failures: 3}
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got healthwatch.State
	if json.Unmarshal(data, &got) != nil || got != want {
		t.Fatal("state lost")
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("unsafe file mode")
	}
}
