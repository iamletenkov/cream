package runner

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// Mask replaces every secret value in filtered output (spec §6.3).
const Mask = "***"

// ErrFilterClosed is returned by Write after Close.
var ErrFilterClosed = errors.New("фильтр секретов закрыт")

// secretFilter is a streaming replacer: it forwards output as soon as it can
// no longer be part of a secret and keeps back only the tail that still might
// begin one (at most the longest secret minus one byte) — so a secret split
// between two writes (two reads of the child's pipe) is masked too, and
// multi-line secrets are matched as a whole. What is masked is the union of
// all occurrences of all secrets: every byte covered by any occurrence is
// hidden, overlapping or nested secrets included, and one run of covered
// bytes becomes one "***".
type secretFilter struct {
	mu      sync.Mutex
	w       io.Writer
	secrets [][]byte // non-empty, unique
	pending []byte
	covered []bool // covered[i]: pending[i] lies inside an occurrence found so far
	inMask  bool   // the last byte written out was covered: a covered run continues
	closed  bool
}

// NewSecretFilter returns a writer that copies to w with every occurrence of
// any of secrets replaced by "***". Empty secrets are ignored; overlapping
// and adjacent occurrences are masked as one run. Close flushes the held-back
// tail and does not close w. With no non-empty secrets the filter is a plain
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
	f.covered = append(f.covered, make([]bool, len(p))...)
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

// flush marks every occurrence of every secret in pending as covered and
// writes out the decided part: everything, if final, or else everything
// before the first position whose rest is a proper prefix of some secret
// (a later write may complete an occurrence starting there).
func (f *secretFilter) flush(final bool) error {
	buf := f.pending
	for _, s := range f.secrets {
		for off := 0; ; {
			j := bytes.Index(buf[off:], s)
			if j < 0 {
				break
			}
			for k := off + j; k < off+j+len(s); k++ {
				f.covered[k] = true
			}
			off += j + 1
		}
	}
	cut := len(buf)
	if !final {
		for i := max(0, len(buf)-f.maxLen()+1); i < len(buf); i++ {
			if f.mightGrow(buf[i:]) {
				cut = i
				break
			}
		}
	}
	var out bytes.Buffer
	for i := 0; i < cut; i++ {
		if !f.covered[i] {
			out.WriteByte(buf[i])
			f.inMask = false
			continue
		}
		if !f.inMask {
			out.WriteString(Mask)
			f.inMask = true
		}
	}
	n := copy(f.pending, buf[cut:])
	f.pending = f.pending[:n]
	copy(f.covered, f.covered[cut:])
	f.covered = f.covered[:n]
	if out.Len() == 0 {
		return nil
	}
	_, err := f.w.Write(out.Bytes())
	return err
}

func (f *secretFilter) maxLen() int {
	n := 0
	for _, s := range f.secrets {
		n = max(n, len(s))
	}
	return n
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
