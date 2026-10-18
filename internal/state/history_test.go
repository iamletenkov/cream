package state

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppend(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t1)
	entries := []HistoryEntry{
		{Time: t0, Op: "install", Ext: "teleport", To: "4.0.0", Status: StatusInstalled, Phase: "build"},
		{Op: "upgrade", Ext: "teleport", From: "4.0.0", To: "4.1.0", Status: StatusFailedUpgrade, Phase: "live"},
	}
	for _, e := range entries {
		if err := s.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	data, err := os.ReadFile(l.History())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-10-18T10:00:00Z","op":"install","ext":"teleport","from":"","to":"4.0.0","status":"installed","phase":"build"}
{"time":"2026-10-18T11:30:00Z","op":"upgrade","ext":"teleport","from":"4.0.0","to":"4.1.0","status":"failed-upgrade","phase":"live"}
`
	if string(data) != want {
		t.Errorf("history.jsonl:\n%s\nwant:\n%s", data, want)
	}
	st, err := os.Stat(l.History())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", st.Mode().Perm())
	}
}

// Every line carries exactly the seven fields of §10: no parameters, ever.
func TestAppendOnlyKnownFields(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	if err := s.Append(HistoryEntry{Op: "remove", Ext: "x", From: "1.0.0", Status: "removed", Phase: "live"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.History())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"time": true, "op": true, "ext": true, "from": true, "to": true, "status": true, "phase": true}
	if len(m) != len(want) {
		t.Errorf("fields = %v, want %v", m, want)
	}
	for k := range m {
		if !want[k] {
			t.Errorf("unexpected field %q", k)
		}
	}
}

// Appends never rewrite what is already there (FROM-chain of images, §10).
func TestAppendKeepsExisting(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	if err := os.MkdirAll(l.InstalledDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"time":"2025-01-01T00:00:00Z","op":"install","ext":"base","from":"","to":"1.0.0","status":"installed","phase":"build"}` + "\n"
	if err := os.WriteFile(l.History(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(HistoryEntry{Time: t0.Add(time.Hour), Op: "install", Ext: "x", To: "1.0.0", Status: "installed", Phase: "live"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.History())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), old) || strings.Count(string(data), "\n") != 2 {
		t.Errorf("history.jsonl = %q", data)
	}
}
