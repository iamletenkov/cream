package fsx

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenTrusted(t *testing.T) {
	t.Parallel()
	me := os.Getuid()
	tests := []struct {
		name    string
		mode    fs.FileMode
		want    fs.FileMode
		owner   int
		setup   func(t *testing.T, top string) string // returns the path to open
		unsafe  bool
		missing bool
	}{
		{name: "secret 0600", mode: 0o600, want: 0o600, owner: me},
		{name: "config 0644", mode: 0o644, want: 0o644, owner: me},
		{name: "tighter than wanted", mode: 0o400, want: 0o644, owner: me},
		{name: "readable by group when 0600 wanted", mode: 0o640, want: 0o600, owner: me, unsafe: true},
		{name: "world writable", mode: 0o666, want: 0o666, owner: me, unsafe: true},
		{name: "group writable", mode: 0o620, want: 0o660, owner: me, unsafe: true},
		{name: "other writable", mode: 0o602, want: 0o606, owner: me, unsafe: true},
		{name: "foreign owner", mode: 0o600, want: 0o600, owner: me + 1, unsafe: true},
		{name: "symlink", mode: 0o600, want: 0o600, owner: me, unsafe: true, setup: func(t *testing.T, top string) string {
			p := filepath.Join(top, "etc", "link")
			if err := os.Symlink("secrets", p); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "group-writable directory", mode: 0o600, want: 0o600, owner: me, unsafe: true, setup: func(t *testing.T, top string) string {
			chmod(t, filepath.Join(top, "etc"), 0o775)
			return filepath.Join(top, "etc", "secrets")
		}},
		{name: "world-writable top", mode: 0o600, want: 0o600, owner: me, unsafe: true, setup: func(t *testing.T, top string) string {
			chmod(t, top, 0o777)
			return filepath.Join(top, "etc", "secrets")
		}},
		{name: "symlinked directory", mode: 0o600, want: 0o600, owner: me, unsafe: true, setup: func(t *testing.T, top string) string {
			if err := os.Symlink("etc", filepath.Join(top, "alias")); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(top, "alias", "secrets")
		}},
		{name: "dot-dot in path", mode: 0o600, want: 0o600, owner: me, setup: func(_ *testing.T, top string) string {
			return top + "/etc/../etc/secrets"
		}},
		{name: "missing", want: 0o600, owner: me, missing: true, setup: func(_ *testing.T, top string) string {
			return filepath.Join(top, "etc", "nope")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			top := t.TempDir()
			chmod(t, top, 0o755)
			etc := filepath.Join(top, "etc")
			if err := os.Mkdir(etc, 0o755); err != nil {
				t.Fatal(err)
			}
			chmod(t, etc, 0o755)
			path := filepath.Join(etc, "secrets")
			if err := os.WriteFile(path, []byte("S=1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.mode != 0 {
				chmod(t, path, tc.mode)
			}
			if tc.setup != nil {
				path = tc.setup(t, top)
			}
			f, err := openTrusted(path, tc.owner, tc.want, top)
			if f != nil {
				defer func() { _ = f.Close() }()
			}
			switch {
			case tc.missing:
				if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrUnsafePerm) {
					t.Errorf("err = %v, want not-exist", err)
				}
			case tc.unsafe:
				if !errors.Is(err, ErrUnsafePerm) {
					t.Errorf("err = %v, want ErrUnsafePerm", err)
				}
				if f != nil {
					t.Error("file returned together with an error")
				}
			default:
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				// The checked descriptor is the one that is read.
				b, err := io.ReadAll(f)
				if err != nil || string(b) != "S=1\n" {
					t.Errorf("read through trusted fd: %q, %v", b, err)
				}
			}
		})
	}
}

func TestCheckOwnerPermFile(t *testing.T) {
	t.Parallel()
	me := os.Getuid()
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := CheckOwnerPermFile(f, me, 0o600); err != nil {
		t.Errorf("own 0600: %v", err)
	}
	if err := CheckOwnerPermFile(f, me+1, 0o600); !errors.Is(err, ErrUnsafePerm) {
		t.Errorf("foreign owner: err = %v, want ErrUnsafePerm", err)
	}
	chmod(t, path, 0o660)
	if err := CheckOwnerPermFile(f, me, 0o660); !errors.Is(err, ErrUnsafePerm) {
		t.Errorf("group writable (same fd, changed mode): err = %v, want ErrUnsafePerm", err)
	}
}

// The exported forms demand root ownership of the file and every directory
// on its path: a file of a non-root test user is refused, a root-owned
// system file passes.
func TestTrustedRequiresRoot(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("тест под root: файл и так принадлежит root")
	}
	path := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnerPerm(path, 0o600); !errors.Is(err, ErrUnsafePerm) {
		t.Errorf("CheckOwnerPerm, non-root owner: err = %v, want ErrUnsafePerm", err)
	}
	if f, err := OpenTrusted(path, 0, 0o600); !errors.Is(err, ErrUnsafePerm) || f != nil {
		t.Errorf("OpenTrusted, non-root owner: %v, %v; want ErrUnsafePerm", f, err)
	}
	if err := CheckOwnerPerm("/etc/passwd", 0o644); err != nil {
		t.Errorf("CheckOwnerPerm(/etc/passwd): %v", err)
	}
	f, err := OpenTrusted("/etc/passwd", 0, 0o644)
	if err != nil {
		t.Fatalf("OpenTrusted(/etc/passwd): %v", err)
	}
	_ = f.Close()
	if _, err := OpenTrusted("etc/passwd", 0, 0o644); err == nil {
		t.Error("relative path: want error")
	}
}
