package engine

import (
	"os"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/manifest"
)

// TestInstallScopeBuildRefused: манифест с ключом scope: build разбирается
// (ADR-0004), но движок до поддержки области build (Task 10 плана
// bisquite-bundled-cream) отказывает install 64 scope.build_unsupported и ничего
// не пишет: значение только для сборки не должно осесть в домене.
func TestInstallScopeBuildRefused(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	te.Cream = manifest.ParseCreamBuild("1.2.0")
	writeFile(t, te, "/etc/os-release", "ID=debian\nVERSION_ID=12\n", 0o644)
	installSync(t, te, installSrc{"contract", testdataDir(t, "contract", "cream-1.2")})

	_, err := installRun(t, te, "scope-build")
	wantCode(t, err, cerr.ExitUsage, "scope.build_unsupported")
	for _, p := range []string{te.L.InstalledFile("scope-build"), te.L.EtcDir("scope-build"), te.L.ExtTree("scope-build")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s существует после отказа (%v)", p, err)
		}
	}
	// Расширение без scope: build из того же каталога ставится.
	installMust(t, te, "every-boot")
}

// TestRefuseScopeBuild: запрет снимается вместе с поддержкой области build.
func TestRefuseScopeBuild(t *testing.T) {
	t.Parallel()
	m := &manifest.Manifest{Name: "x", Config: keytype.Schema{
		"A": {Scope: keytype.ScopeConfig}, "B": {Scope: keytype.ScopeInstall},
	}}
	if err := refuseScopeBuild(m); err != nil {
		t.Fatalf("config/install: %v", err)
	}
	m.Config["TOKEN"] = keytype.Spec{Scope: keytype.ScopeBuild}
	wantCode(t, refuseScopeBuild(m), cerr.ExitUsage, "scope.build_unsupported")
}
