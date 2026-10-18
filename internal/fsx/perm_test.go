package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckOwnerPerm(t *testing.T) {
	t.Parallel()
	me := os.Getuid()
	tests := []struct {
		name    string
		mode    fs.FileMode
		want    fs.FileMode
		owner   int
		link    bool
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
		{name: "symlink", mode: 0o600, want: 0o600, owner: me, link: true, unsafe: true},
		{name: "missing", want: 0o600, owner: me, missing: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "secrets")
			if !tc.missing {
				target := path
				if tc.link {
					target = filepath.Join(dir, "real")
				}
				if err := os.WriteFile(target, []byte("S=1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				chmod(t, target, tc.mode)
				if tc.link {
					if err := os.Symlink("real", path); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := checkOwnerPerm(path, tc.want, tc.owner)
			switch {
			case tc.missing:
				if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrUnsafePerm) {
					t.Errorf("err = %v, want not-exist", err)
				}
			case tc.unsafe:
				if !errors.Is(err, ErrUnsafePerm) {
					t.Errorf("err = %v, want ErrUnsafePerm", err)
				}
			default:
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			}
		})
	}
}

// The exported form demands root ownership: a file of a non-root test user
// is refused, a root-owned system file passes.
func TestCheckOwnerPermRequiresRoot(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("тест под root: файл и так принадлежит root")
	}
	path := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnerPerm(path, 0o600); !errors.Is(err, ErrUnsafePerm) {
		t.Errorf("non-root owner: err = %v, want ErrUnsafePerm", err)
	}
	if err := CheckOwnerPerm("/etc/passwd", 0o644); err != nil {
		t.Errorf("/etc/passwd: %v", err)
	}
}
