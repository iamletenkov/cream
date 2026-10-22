package resolve

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/state"
)

const (
	gitCommit = "git:1a2b3c4d5e6f7081920a1b2c3d4e5f60718293a4"
	extDigest = "sha256:5d1f0c7e9a8b6c4d2e0f1a3b5c7d9e1f2a4b6c8d0e2f4a6b8c0d2e4f6a8b0c2d"
)

// TestLayerGolden checks the resolve --json layer against the example of §12.4.
func TestLayerGolden(t *testing.T) {
	t.Parallel()
	desktop := installed(synth(t, "desktop", "4.0.0", "provides: [desktop-session]"))
	x11vnc := synth(t, "x11vnc", "4.0.0", `depends: [{name: desktop, version: ">=4.0.0 <5"}]
config:
  keys:
    VNC_PORT: {type: port, default: "5900", description: порт}
    VNC_PASSWORD: {type: str, default: "", empty: true, secret: file, description: пароль}`)
	x11vnc.Source = state.SourceRef{Name: "core", Type: "git", Origin: "https://example/cream-extensions.git",
		Resolved: gitCommit, CatalogVersion: "4.0.0"}
	x11vnc.Digest = extDigest
	params := map[string]map[string]string{"x11vnc": {"VNC_PORT": "5901", "VNC_PASSWORD": "hunter2"}}
	h := liveHost()
	h.Phase = PhaseBuild
	ls, err := Resolve([]Node{desktop}, []Node{x11vnc}, params, h, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "layer-x11vnc.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("resolve --json:\n%s\nждали:\n%s", got, want)
	}
	if bytes.Contains(got, []byte("hunter2")) {
		t.Fatal("значение секрета в слое")
	}
	wantDigest := state.ParamsDigest(params["x11vnc"], func(k string) bool { return k == "VNC_PASSWORD" })
	if ls[0].ParamsDigest != wantDigest {
		t.Fatalf("params_digest = %s, ждали %s", ls[0].ParamsDigest, wantDigest)
	}
}

func TestLayerFields(t *testing.T) {
	t.Parallel()
	reTok := regexp.MustCompile(`^[a-z0-9._-]+/[a-z0-9._-]+@[0-9][^#]*#[0-9a-f]{12}$`)
	withSrc := func(n Node, typ, resolved string) Node {
		n.Source = state.SourceRef{Name: "src", Type: typ, Resolved: resolved, CatalogVersion: "1.0.0"}
		return n
	}
	tests := []struct {
		name       string
		node       Node
		params     map[string]string
		wantToken  string
		wantPinned bool
		wantSecret []string
	}{
		{name: "archive — пин, sha256 в токене", node: withSrc(fixture(t, "noop"), "archive", "sha256:"+strings.Repeat("9f", 32)),
			wantToken: "src/noop@1.0.0#9f9f9f9f9f9f", wantPinned: true, wantSecret: []string{}},
		{name: "git — пин", node: withSrc(fixture(t, "noop"), "git", gitCommit),
			wantToken: "src/noop@1.0.0#1a2b3c4d5e6f", wantPinned: true, wantSecret: []string{}},
		{name: "path — не пин", node: withSrc(fixture(t, "noop"), "path", "sha256:"+strings.Repeat("0a", 32)),
			wantToken: "src/noop@1.0.0#0a0a0a0a0a0a", wantSecret: []string{}},
		{name: "секретные параметры помечены, шаблон тоже", node: withSrc(fixture(t, "with-config"), "path", "sha256:"+strings.Repeat("0a", 32)),
			params:    map[string]string{"TOKEN": "s3cr3t", "COUNT": "1", "LBL_x": "y"},
			wantToken: "src/with-config@1.0.0#0a0a0a0a0a0a", wantSecret: []string{"TOKEN"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			params := map[string]map[string]string{tc.node.Manifest.Name: tc.params}
			ls, err := Resolve(nil, []Node{tc.node}, params, liveHost(), nil)
			if err != nil {
				t.Fatal(err)
			}
			l := ls[0]
			if l.Token != tc.wantToken || !reTok.MatchString(l.Token) {
				t.Fatalf("token = %q, ждали %q", l.Token, tc.wantToken)
			}
			if l.Pinned != tc.wantPinned || !slices.Equal(l.SecretParams, tc.wantSecret) || l.SecretParams == nil {
				t.Fatalf("pinned = %v, secret_params = %#v", l.Pinned, l.SecretParams)
			}
			if l.Deps == nil || l.ExtDigest != tc.node.Digest || l.CatalogVersion != "1.0.0" || l.Source != "src" {
				t.Fatalf("слой = %+v", l)
			}
			if want := state.ParamsDigest(tc.params, func(k string) bool { return slices.Contains(tc.wantSecret, k) }); l.ParamsDigest != want {
				t.Fatalf("params_digest = %s, ждали %s", l.ParamsDigest, want)
			}
		})
	}
}

func TestLayerWithoutSource(t *testing.T) {
	t.Parallel()
	// --from-dir без источника: токен без источника и ревизии.
	n := fixture(t, "noop")
	n.Source = state.SourceRef{}
	ls, err := Resolve(nil, []Node{n}, nil, liveHost(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if ls[0].Token != "noop@1.0.0" || ls[0].Pinned {
		t.Fatalf("слой = %+v", ls[0])
	}
}

func TestLayerDeps(t *testing.T) {
	t.Parallel()
	// deps: цели depends и поставщики requires — имя@версия, отсортировано.
	rt := installed(fixture(t, "apply-hook"))
	other := installed(synth(t, "other-rt", "2.1.0", "provides: [fixture-runtime]"))
	lib := installed(fixture(t, "lib-fixture"))
	top := synth(t, "top", "1.0.0", "depends: [{name: lib-fixture}]\nrequires: [fixture-runtime]")
	ls, err := Resolve([]Node{rt, other, lib}, []Node{top}, nil, liveHost(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"apply-hook@1.0.0", "lib-fixture@1.0.0", "other-rt@2.1.0"}
	if !slices.Equal(ls[0].Deps, want) {
		t.Fatalf("deps = %v, ждали %v", ls[0].Deps, want)
	}
}

// TestLayerParamsDigestSkipsScopeBuild: ключи scope: build не входят ни в
// params_digest, ни в secret_params (спека 2026-10-21 §10.9).
func TestLayerParamsDigestSkipsScopeBuild(t *testing.T) {
	t.Parallel()
	n := fixture(t, "with-config")
	n.Manifest.Config["MIRROR_TOKEN"] = keytype.Spec{Empty: true, Secret: keytype.SecretFile, Scope: keytype.ScopeBuild}
	n.Manifest.Config["MIRROR_URL"] = keytype.Spec{Empty: true, Scope: keytype.ScopeBuild}
	params := map[string]map[string]string{n.Manifest.Name: {"COUNT": "1", "MIRROR_TOKEN": "t", "MIRROR_URL": "u"}}
	ls, err := Resolve(nil, []Node{n}, params, liveHost(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := state.ParamsDigest(map[string]string{"COUNT": "1"}, nil); ls[0].ParamsDigest != want {
		t.Errorf("params_digest = %s, ждали %s", ls[0].ParamsDigest, want)
	}
	if len(ls[0].SecretParams) != 0 {
		t.Errorf("secret_params = %v", ls[0].SecretParams)
	}
}
