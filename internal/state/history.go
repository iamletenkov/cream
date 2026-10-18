package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
)

// HistoryEntry is one line of history.jsonl. It has no parameters by design:
// the journal travels with images along the FROM chain (§10).
type HistoryEntry struct {
	Time   time.Time `json:"time"`
	Op     string    `json:"op"`
	Ext    string    `json:"ext"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Status string    `json:"status"`
	Phase  string    `json:"phase"`
}

// Append adds one line to the journal (mode 0644), creating it when absent.
// A zero Time is stamped with now; times are UTC seconds. The line goes out
// in a single write to an O_APPEND file, so concurrent writers never interleave.
func (s *Store) Append(e HistoryEntry) error {
	if e.Time.IsZero() {
		e.Time = s.now()
	}
	e.Time = e.Time.UTC().Truncate(time.Second)
	line, err := json.Marshal(e)
	if err != nil {
		return cerr.Wrap(err, cerr.ExitInternal, cerr.CodeInternal, "журнал операций")
	}
	line = append(line, '\n')
	path := s.layout.History()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "журнал %s", path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "журнал %s", path)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return cerr.Wrap(err, cerr.ExitState, "state.write", "журнал %s", path)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return cerr.Wrap(err, cerr.ExitState, "state.write", "журнал %s: fsync", path)
	}
	if err := f.Close(); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "журнал %s", path)
	}
	return nil
}
