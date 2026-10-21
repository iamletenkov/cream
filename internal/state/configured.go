package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
)

// Configured is the configure run mark /var/lib/cream/configured/<ext>.json
// (spec bisquite-bundled-cream §10.7): written after a successful configure,
// compared with the registry at boot. It carries no setting values:
// ParamsDigest is the digest of the non-secret settings, SecretsRev the
// counter of secret writes (Record.SecretsRev), MachineID the sha256 of
// /etc/machine-id.
type Configured struct {
	Version      string    `json:"version"`
	ExtDigest    string    `json:"ext_digest"`
	ParamsDigest string    `json:"params_digest"`
	SecretsRev   int       `json:"secrets_rev"`
	MachineID    string    `json:"machine_id"`
	At           time.Time `json:"at"`
}

// Same reports whether c and o describe the same configured state: every
// field but At is equal.
func (c Configured) Same(o Configured) bool {
	c.At, o.At = time.Time{}, time.Time{}
	return c == o
}

// GetConfigured returns the configure mark of name; ok is false when there
// is none. A mark that cannot be decoded counts as absent: the hook runs
// again and rewrites it.
func (s *Store) GetConfigured(name string) (c *Configured, ok bool, err error) {
	if err := checkName(name); err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(s.layout.ConfiguredFile(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "отметка configure %s", name)
	}
	var out Configured
	if json.Unmarshal(data, &out) != nil {
		return nil, false, nil
	}
	return &out, true, nil
}

// PutConfigured writes the configure mark of name atomically (mode 0644);
// At is set to now.
func (s *Store) PutConfigured(name string, c Configured) error {
	if err := checkName(name); err != nil {
		return err
	}
	c.At = s.stamp()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return cerr.Wrap(err, cerr.ExitInternal, cerr.CodeInternal, "отметка configure %s", name)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.layout.ConfiguredDir(), 0o755); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "отметка configure %s", name)
	}
	if err := s.write(s.layout.ConfiguredFile(name), data, 0o644); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "отметка configure %s", name)
	}
	return nil
}

// DeleteConfigured removes the configure mark of name; a missing mark is not
// an error.
func (s *Store) DeleteConfigured(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	err := os.Remove(s.layout.ConfiguredFile(name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "удаление отметки configure %s", name)
	}
	return nil
}

// maxMachineID bounds what is read from /etc/machine-id.
const maxMachineID = 256

// MachineID is "sha256:<hex>" of /etc/machine-id (trimmed), the identity of
// the machine in the configure mark: a clone with a regenerated machine-id
// configures again. A missing or unreadable file is "".
func (s *Store) MachineID() string {
	f, err := os.Open(s.layout.MachineID())
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxMachineID))
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return "sha256:" + hex.EncodeToString(sum[:])
}
