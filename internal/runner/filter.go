package runner

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"sync"
)

// Mask replaces every secret value in filtered output (spec §6.3).
const Mask = "***"

// ErrFilterClosed is returned by Write after Close.
var ErrFilterClosed = errors.New("фильтр секретов закрыт")

// secretFilter is a streaming replacer: it forwards output as soon as it can
// no longer be the beginning of a secret and keeps back only the tail that
// still might be — so a secret split between two writes (two reads of the
// child's pipe) is masked too, and multi-line secrets are matched as a whole.
type secretFilter struct {
	mu      sync.Mutex
	w       io.Writer
	secrets [][]byte // non-empty, longest first
	pending []byte
	closed  bool
}

// NewSecretFilter returns a writer that copies to w with every occurrence of
// any of secrets replaced by "***". Empty secrets are ignored; where secrets
// overlap at one position, the longest wins. Close flushes the held-back tail
// and does not close w. With no non-empty secrets the filter is a plain
// pass-through (still with Close).
func NewSecretFilter(w io.Writer, secrets []string) io.WriteCloser {
	f := &secretFilter{w: w}
	seen := map[string]bool{}
	for _, s := range secrets {
		if s != "" && !seen[s] {
			seen[s] = true
			f.secrets = append(f.secrets, []byte(s))
		}
	}
	slices.SortFunc(f.secrets, func(a, b []byte) int { return len(b) - len(a) })
	return f
}

// Write filters p; it reports len(p) on success even though fewer or more
// bytes may reach w.
func (f *secretFilter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, ErrFilterClosed
	}
	if len(f.secrets) == 0 {
		return f.w.Write(p)
	}
	f.pending = append(f.pending, p...)
	if err := f.flush(false); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close writes out what was held back. Repeated calls do nothing.
func (f *secretFilter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return f.flush(true)
}

// flush masks pending and writes all of it that is decided; unless final, a
// tail that is a proper prefix of some secret stays in pending.
func (f *secretFilter) flush(final bool) error {
	buf := f.pending
	var out bytes.Buffer
	start := 0 // beginning of the not yet copied plain run
	i := 0
	for i < len(buf) {
		rest := buf[i:]
		if !final && f.mightGrow(rest) {
			break
		}
		if n := f.matchAt(rest); n > 0 {
			out.Write(buf[start:i])
			out.WriteString(Mask)
			i += n
			start = i
			continue
		}
		i++
	}
	out.Write(buf[start:i])
	f.pending = append(f.pending[:0], buf[i:]...)
	if out.Len() == 0 {
		return nil
	}
	_, err := f.w.Write(out.Bytes())
	return err
}

// matchAt returns the length of the longest secret rest starts with, or 0.
func (f *secretFilter) matchAt(rest []byte) int {
	for _, s := range f.secrets {
		if bytes.HasPrefix(rest, s) {
			return len(s)
		}
	}
	return 0
}

// mightGrow reports whether rest is a proper prefix of some secret: more
// input may complete it, so nothing from here on is decided yet.
func (f *secretFilter) mightGrow(rest []byte) bool {
	for _, s := range f.secrets {
		if len(rest) < len(s) && bytes.HasPrefix(s, rest) {
			return true
		}
	}
	return false
}
