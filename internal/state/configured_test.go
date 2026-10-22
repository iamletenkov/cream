package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/paths"
)

func newConfiguredStore(t *testing.T) (*Store, paths.Layout) {
	t.Helper()
	l := paths.Layout{Root: t.TempDir()}
	now := time.Date(2026, 10, 21, 10, 0, 0, 500, time.UTC)
	return NewStore(l, func() time.Time { return now }), l
}

func sampleConfigured() Configured {
	return Configured{Version: "2.1.0", ExtDigest: "sha256:aa", ParamsDigest: "sha256:bb",
		SecretsRev: 3, MachineID: "sha256:cc"}
}

func TestConfiguredRoundTrip(t *testing.T) {
	t.Parallel()
	s, l := newConfiguredStore(t)
	if _, ok, err := s.GetConfigured("desktop"); ok || err != nil {
		t.Fatalf("нет отметки: ok=%v err=%v", ok, err)
	}
	if err := s.PutConfigured("desktop", sampleConfigured()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetConfigured("desktop")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	want := sampleConfigured()
	want.At = time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if *got != want {
		t.Errorf("отметка %+v, ждали %+v", *got, want)
	}
	st, err := os.Stat(l.ConfiguredFile("desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Errorf("права %v, ждали 0644", st.Mode().Perm())
	}
	// Форма файла — ровно поля спеки §10.7, без значений настроек.
	data, _ := os.ReadFile(l.ConfiguredFile("desktop"))
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"at", "ext_digest", "machine_id", "params_digest", "secrets_rev", "version"}; !slices.Equal(keys, want) {
		t.Errorf("поля %v, ждали %v", keys, want)
	}
	// Атомарно: временных файлов рядом нет.
	ents, _ := os.ReadDir(l.ConfiguredDir())
	if len(ents) != 1 {
		t.Errorf("в каталоге отметок лишнее: %v", ents)
	}
}

func TestConfiguredDelete(t *testing.T) {
	t.Parallel()
	s, l := newConfiguredStore(t)
	if err := s.DeleteConfigured("desktop"); err != nil {
		t.Fatalf("удаление отсутствующей отметки: %v", err)
	}
	if err := s.PutConfigured("desktop", sampleConfigured()); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteConfigured("desktop"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.ConfiguredFile("desktop")); !os.IsNotExist(err) {
		t.Errorf("отметка осталась: %v", err)
	}
}

func TestConfiguredCorruptIsAbsent(t *testing.T) {
	t.Parallel()
	s, l := newConfiguredStore(t)
	if err := os.MkdirAll(l.ConfiguredDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.ConfiguredFile("desktop"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetConfigured("desktop"); ok || err != nil {
		t.Errorf("испорченная отметка: ok=%v err=%v — ждали «нет отметки»", ok, err)
	}
}

func TestConfiguredBadName(t *testing.T) {
	t.Parallel()
	s, _ := newConfiguredStore(t)
	for _, name := range []string{"", "../x", ".hidden"} {
		if err := s.PutConfigured(name, sampleConfigured()); err == nil {
			t.Errorf("PutConfigured(%q) без ошибки", name)
		}
		if _, _, err := s.GetConfigured(name); err == nil {
			t.Errorf("GetConfigured(%q) без ошибки", name)
		}
		if err := s.DeleteConfigured(name); err == nil {
			t.Errorf("DeleteConfigured(%q) без ошибки", name)
		}
	}
}

func TestConfiguredSame(t *testing.T) {
	t.Parallel()
	base := sampleConfigured()
	other := base
	other.At = time.Now()
	if !base.Same(other) {
		t.Error("at не участвует в сравнении")
	}
	for name, f := range map[string]func(*Configured){
		"version":       func(c *Configured) { c.Version = "2.2.0" },
		"ext_digest":    func(c *Configured) { c.ExtDigest = "sha256:ab" },
		"params_digest": func(c *Configured) { c.ParamsDigest = "sha256:bc" },
		"secrets_rev":   func(c *Configured) { c.SecretsRev++ },
		"machine_id":    func(c *Configured) { c.MachineID = "sha256:cd" },
	} {
		c := base
		f(&c)
		if base.Same(c) {
			t.Errorf("%s: отличие не замечено", name)
		}
	}
}

func TestMachineIDDigest(t *testing.T) {
	t.Parallel()
	s, l := newConfiguredStore(t)
	if got := s.MachineID(); got != "" {
		t.Errorf("нет /etc/machine-id: %q, ждали пусто", got)
	}
	id := "machine-of-the-test-host"
	if err := os.MkdirAll(filepath.Dir(l.MachineID()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.MachineID(), []byte(id+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(id))
	want := "sha256:" + hex.EncodeToString(sum[:])
	got := s.MachineID()
	if got != want {
		t.Errorf("machine_id %q, ждали %q", got, want)
	}
	if strings.Contains(got, id) {
		t.Error("сам идентификатор попал в отметку")
	}
}
