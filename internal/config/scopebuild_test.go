package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/paths"
)

// buildSchema — домен с ключами scope: build (ADR-0004, спека §10.9) рядом с
// обычными.
func buildSchema(t *testing.T) keytype.Schema {
	t.Helper()
	return keytype.Schema{
		"LEVEL":     {Type: mustType(t, "str"), Default: "a", Description: "обычный", Scope: keytype.ScopeConfig},
		"TOKEN":     {Type: mustType(t, "str"), Empty: true, Secret: keytype.SecretFile, Scope: keytype.ScopeBuild, Description: "токен"},
		"MIRROR":    {Type: mustType(t, "str"), Default: "http://default", Scope: keytype.ScopeBuild, Description: "зеркало"},
		"REQUIRED":  {Type: mustType(t, "str"), Scope: keytype.ScopeBuild, Description: "обязательный"},
		"COUNT":     {Type: mustType(t, "int"), Default: "1", Scope: keytype.ScopeBuild, Description: "число"},
		"BUILD_*":   {Type: mustType(t, "str"), Pattern: true, Scope: keytype.ScopeBuild, Description: "шаблон"},
		"INSTALLED": {Type: mustType(t, "str"), Default: "v", Scope: keytype.ScopeInstall, Description: "установка"},
	}
}

func wantErrCode(t *testing.T, err error, exit int, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("ошибки нет, ждали %d %s", exit, code)
	}
	if cerr.ExitCode(err) != exit || cerr.CodeOf(err) != code {
		t.Fatalf("%d %s (%v), ждали %d %s", cerr.ExitCode(err), cerr.CodeOf(err), err, exit, code)
	}
}

// TestDomainScopeBuildNeverStored: домен не принимает и не отдаёт ключи
// scope: build — set/get → 64 config.scope_build, в show, Values, export и
// Secrets их нет (их видит только хук install/upgrade).
func TestDomainScopeBuildNeverStored(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	d := Open(l, testExt, buildSchema(t))
	for _, k := range []string{"TOKEN", "MIRROR", "BUILD_X"} {
		for _, o := range []SetOpts{{}, {AllowInstallScope: true, AllowReadonly: true}} {
			_, err := d.Set([]Change{{Key: "LEVEL", Value: "b"}, {Key: k, Value: "x"}}, o)
			wantErrCode(t, err, cerr.ExitUsage, "config.scope_build")
		}
		_, err := d.Get(k, true)
		wantErrCode(t, err, cerr.ExitUsage, "config.scope_build")
	}
	if _, err := os.Stat(l.EtcDir(testExt)); !os.IsNotExist(err) {
		t.Fatalf("каталог настроек создан после отказа: %v", err)
	}

	// Ключ, попавший в файл руками, тоже не всплывает: show его не печатает,
	// Values/export не отдают, Secrets не маскирует (он не хранится вовсе).
	writeFile(t, l.ConfigFile(testExt), "LEVEL=b\nMIRROR=http://hand\n", 0o644)
	vals, err := d.Values()
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"LEVEL": "b", "INSTALLED": "v"}; !reflect.DeepEqual(vals, want) {
		t.Errorf("Values = %v, ждали %v", vals, want)
	}
	rows, err := d.Show()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key != "LEVEL" && r.Key != "INSTALLED" {
			t.Errorf("show: строка %+v", r)
		}
	}
	sh, err := d.ExportShell()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sh, "MIRROR") || strings.Contains(sh, "TOKEN") {
		t.Errorf("export: %q", sh)
	}
}

// TestSplitBuild: присваивания делятся на хранимые и только для сборки;
// значения сборки проверяются по типу, сообщение не цитирует секрет.
func TestSplitBuild(t *testing.T) {
	t.Parallel()
	s := buildSchema(t)
	stored, build, err := SplitBuild([]Change{{Key: "LEVEL", Value: "b"}, {Key: "TOKEN", Value: "t0p"},
		{Key: "INSTALLED", Value: "w"}, {Key: "BUILD_X", Value: "y"}}, s)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Change{{Key: "LEVEL", Value: "b"}, {Key: "INSTALLED", Value: "w"}}; !reflect.DeepEqual(stored, want) {
		t.Errorf("stored = %v", stored)
	}
	if want := []Change{{Key: "TOKEN", Value: "t0p"}, {Key: "BUILD_X", Value: "y"}}; !reflect.DeepEqual(build, want) {
		t.Errorf("build = %v", build)
	}

	_, _, err = SplitBuild([]Change{{Key: "COUNT", Value: "много"}}, s)
	wantErrCode(t, err, cerr.ExitData, "config.invalid")
	sec := keytype.Schema{"TOKEN": {Type: mustType(t, "int"), Secret: keytype.SecretFile, Scope: keytype.ScopeBuild, Description: "т"}}
	_, _, err = SplitBuild([]Change{{Key: "TOKEN", Value: "secret-marker"}}, sec)
	wantErrCode(t, err, cerr.ExitData, "config.invalid")
	if strings.Contains(err.Error(), "secret-marker") {
		t.Errorf("секрет в сообщении: %v", err)
	}
	_, _, err = SplitBuild([]Change{{Key: "MIRROR", Value: "a\x00b"}}, s)
	wantErrCode(t, err, cerr.ExitData, "config.invalid")
}

// TestBuildValues: окружение сборки — умолчания ключей scope: build под
// заданными значениями; обязательный ключ без значения — 64
// config.build_required, когда required.
func TestBuildValues(t *testing.T) {
	t.Parallel()
	s := buildSchema(t)
	got, err := BuildValues(s, []Change{{Key: "TOKEN", Value: "t"}, {Key: "REQUIRED", Value: "r"}, {Key: "BUILD_X", Value: "y"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"TOKEN": "t", "MIRROR": "http://default", "REQUIRED": "r", "COUNT": "1", "BUILD_X": "y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildValues = %v, ждали %v", got, want)
	}
	_, err = BuildValues(s, nil, true)
	wantErrCode(t, err, cerr.ExitUsage, "config.build_required")
	got, err = BuildValues(s, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["REQUIRED"]; ok {
		t.Errorf("обязательный без значения в окружении: %v", got)
	}
	if got["TOKEN"] != "" || got["MIRROR"] != "http://default" {
		t.Errorf("умолчания: %v", got)
	}
}
