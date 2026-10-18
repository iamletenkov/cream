package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/paths"
)

const testExt = "demo"

func mustType(t *testing.T, s string) keytype.Type {
	t.Helper()
	ty, err := keytype.ParseType(s)
	if err != nil {
		t.Fatalf("ParseType(%q): %v", s, err)
	}
	return ty
}

func testSchema(t *testing.T) keytype.Schema {
	t.Helper()
	return keytype.Schema{
		"PORT":    {Type: mustType(t, "port"), Default: "8080", Description: "порт", Scope: keytype.ScopeConfig},
		"HOST":    {Type: mustType(t, "host"), Empty: true, Description: "хост"},
		"MODE":    {Type: mustType(t, "enum:a,b"), Default: "a", Description: "режим"},
		"FLAG":    {Type: mustType(t, "bool"), Default: "0", Description: "флаг"},
		"TOKEN":   {Type: mustType(t, "str"), Empty: true, Secret: keytype.SecretFile, Description: "токен"},
		"RO":      {Type: mustType(t, "str"), Default: "x", Readonly: true, Description: "только чтение"},
		"VERSION": {Type: mustType(t, "str"), Default: "1.0", Scope: keytype.ScopeInstall, Description: "версия"},
		"LABEL_*": {Type: mustType(t, "str"), Pattern: true, Description: "метки"},
	}
}

func newDomain(t *testing.T) (*Domain, paths.Layout) {
	t.Helper()
	l := paths.Layout{Root: t.TempDir()}
	return Open(l, testExt, testSchema(t)), l
}

func writeFile(t *testing.T, path, data string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

func wantCerr(t *testing.T, err error, exit int, code string) {
	t.Helper()
	if exit == 0 {
		if err != nil {
			t.Fatalf("ошибка не ожидалась: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("ожидали ошибку %d %s, получили nil", exit, code)
	}
	if got := cerr.ExitCode(err); got != exit {
		t.Fatalf("код выхода %d, ожидали %d (%v)", got, exit, err)
	}
	if got := cerr.CodeOf(err); got != code {
		t.Fatalf("error.code %q, ожидали %q (%v)", got, code, err)
	}
}

func TestSet(t *testing.T) {
	t.Parallel()
	const operatorConfig = "# настроено оператором\nPORT=9000\n\n; ещё комментарий\nMODE=b\n"
	tests := []struct {
		name        string
		config      string // исходный config ("" — файла нет)
		changes     []Change
		opts        SetOpts
		wantExit    int
		wantCode    string
		wantChanged []string
		wantConfig  string // "" — config не должен появиться/меняться (см. untouched)
		wantSecrets string
		untouched   bool // config должен остаться ровно исходным (или отсутствовать)
	}{
		{
			name:        "валидный набор",
			changes:     []Change{{"PORT", "8443"}, {"HOST", "proxy.example:443"}, {"FLAG", "1"}},
			wantChanged: []string{"PORT", "HOST", "FLAG"},
			wantConfig:  "PORT=8443\nHOST=proxy.example:443\nFLAG=1\n",
		},
		{
			name:      "один невалидный ключ — ничего не записано",
			config:    operatorConfig,
			changes:   []Change{{"PORT", "8443"}, {"FLAG", "true"}},
			wantExit:  cerr.ExitData,
			wantCode:  "config.invalid",
			untouched: true,
		},
		{
			name:      "пустое значение без empty",
			changes:   []Change{{"PORT", ""}},
			wantExit:  cerr.ExitData,
			wantCode:  "config.invalid",
			untouched: true,
		},
		{
			name:      "неизвестный ключ",
			changes:   []Change{{"NOPE", "1"}},
			wantExit:  cerr.ExitData,
			wantCode:  "config.unknown_key",
			untouched: true,
		},
		{
			name:      "имя по шаблону, недопустимое для EnvironmentFile",
			changes:   []Change{{"LABEL_a-b", "x"}},
			wantExit:  cerr.ExitData,
			wantCode:  "config.unknown_key",
			untouched: true,
		},
		{
			name:        "ключ по шаблону",
			changes:     []Change{{"LABEL_env", "prod"}},
			wantChanged: []string{"LABEL_env"},
			wantConfig:  "LABEL_env=prod\n",
		},
		{
			name:      "значение с NUL",
			changes:   []Change{{"LABEL_x", "a\x00b"}},
			wantExit:  cerr.ExitData,
			wantCode:  "config.invalid",
			untouched: true,
		},
		{
			name:      "дубль ключа в одном вызове",
			changes:   []Change{{"PORT", "1"}, {"PORT", "2"}},
			wantExit:  cerr.ExitUsage,
			wantCode:  "usage.duplicate_key",
			untouched: true,
		},
		{
			name:      "readonly без AllowReadonly",
			changes:   []Change{{"RO", "y"}},
			wantExit:  cerr.ExitData,
			wantCode:  "config.readonly",
			untouched: true,
		},
		{
			name:        "readonly с AllowReadonly",
			changes:     []Change{{"RO", "y"}},
			opts:        SetOpts{AllowReadonly: true},
			wantChanged: []string{"RO"},
			wantConfig:  "RO=y\n",
		},
		{
			name:      "scope install без AllowInstallScope",
			changes:   []Change{{"VERSION", "2.0"}},
			wantExit:  cerr.ExitData,
			wantCode:  "config.install_scope",
			untouched: true,
		},
		{
			name:      "AllowReadonly не снимает scope install",
			changes:   []Change{{"VERSION", "2.0"}},
			opts:      SetOpts{AllowReadonly: true},
			wantExit:  cerr.ExitData,
			wantCode:  "config.install_scope",
			untouched: true,
		},
		{
			name:        "scope install с AllowInstallScope",
			changes:     []Change{{"VERSION", "2.0"}},
			opts:        SetOpts{AllowInstallScope: true},
			wantChanged: []string{"VERSION"},
			wantConfig:  "VERSION=2.0\n",
		},
		{
			name:        "секрет уходит в secrets",
			config:      "PORT=9000\n",
			changes:     []Change{{"TOKEN", "s3cr3t value"}, {"PORT", "9001"}},
			wantChanged: []string{"TOKEN", "PORT"},
			wantConfig:  "PORT=9001\n",
			wantSecrets: "TOKEN=\"s3cr3t value\"\n",
		},
		{
			name:        "секрет, ошибочно лежавший в config, оттуда убирается",
			config:      "TOKEN=old\nPORT=9000\n",
			changes:     []Change{{"TOKEN", "new"}},
			wantChanged: []string{"TOKEN"},
			wantConfig:  "PORT=9000\n",
			wantSecrets: "TOKEN=new\n",
		},
		{
			name:        "комментарии оператора сохраняются",
			config:      operatorConfig,
			changes:     []Change{{"MODE", "a"}, {"HOST", "h.example"}},
			wantChanged: []string{"MODE", "HOST"},
			wantConfig:  "# настроено оператором\nPORT=9000\n\n; ещё комментарий\nMODE=a\nHOST=h.example\n",
		},
		{
			name:        "changed — только реально изменённые",
			config:      operatorConfig,
			changes:     []Change{{"PORT", "9000"}, {"MODE", "a"}},
			wantChanged: []string{"MODE"},
			wantConfig:  "# настроено оператором\nPORT=9000\n\n; ещё комментарий\nMODE=a\n",
		},
		{
			name:        "ничего не изменилось — файл не трогается",
			config:      operatorConfig,
			changes:     []Change{{"PORT", "9000"}},
			wantChanged: nil,
			untouched:   true,
		},
		{
			name:        "Only: проверяются все, пишутся только перечисленные",
			changes:     []Change{{"PORT", "1"}, {"VERSION", "3.0"}},
			opts:        SetOpts{AllowInstallScope: true, Only: []string{"VERSION"}},
			wantChanged: []string{"VERSION"},
			wantConfig:  "VERSION=3.0\n",
		},
		{
			name:      "Only: невалидный неперечисленный ключ — всё равно отказ",
			changes:   []Change{{"PORT", "x"}, {"VERSION", "3.0"}},
			opts:      SetOpts{AllowInstallScope: true, Only: []string{"VERSION"}},
			wantExit:  cerr.ExitData,
			wantCode:  "config.invalid",
			untouched: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, l := newDomain(t)
			if tc.config != "" {
				writeFile(t, l.ConfigFile(testExt), tc.config, 0o644)
			}
			changed, err := d.Set(tc.changes, tc.opts)
			wantCerr(t, err, tc.wantExit, tc.wantCode)
			if err == nil && !reflect.DeepEqual(changed, tc.wantChanged) {
				t.Errorf("changed = %q, ожидали %q", changed, tc.wantChanged)
			}
			cfg, cfgOK := readFile(t, l.ConfigFile(testExt))
			switch {
			case tc.untouched:
				if cfg != tc.config || cfgOK != (tc.config != "") {
					t.Errorf("config изменён: %q (был %q)", cfg, tc.config)
				}
			default:
				if cfg != tc.wantConfig {
					t.Errorf("config = %q, ожидали %q", cfg, tc.wantConfig)
				}
			}
			sec, secOK := readFile(t, l.SecretsFile(testExt))
			if sec != tc.wantSecrets {
				t.Errorf("secrets = %q, ожидали %q", sec, tc.wantSecrets)
			}
			if strings.Contains(cfg, "s3cr3t") {
				t.Errorf("секрет попал в config: %q", cfg)
			}
			if secOK {
				st, err := os.Stat(l.SecretsFile(testExt))
				if err != nil {
					t.Fatal(err)
				}
				if st.Mode().Perm() != 0o600 {
					t.Errorf("права secrets %04o, ожидали 0600", st.Mode().Perm())
				}
			}
			if cfgOK && !tc.untouched {
				st, err := os.Stat(l.ConfigFile(testExt))
				if err != nil {
					t.Fatal(err)
				}
				if st.Mode().Perm() != 0o644 {
					t.Errorf("права config %04o, ожидали 0644", st.Mode().Perm())
				}
			}
		})
	}
}

func TestSetKeepsOtherSecrets(t *testing.T) {
	t.Parallel()
	schema := testSchema(t)
	schema["PASS"] = keytype.Spec{Type: mustType(t, "str"), Empty: true, Secret: keytype.SecretFile, Description: "пароль"}
	l := paths.Layout{Root: t.TempDir()}
	writeFile(t, l.SecretsFile(testExt), "# секреты\nPASS=keep\n", 0o600)
	d := Open(l, testExt, schema)
	if _, err := d.Set([]Change{{"TOKEN", "t"}}, SetOpts{}); err != nil {
		t.Fatal(err)
	}
	got, _ := readFile(t, l.SecretsFile(testExt))
	if want := "# секреты\nPASS=keep\nTOKEN=t\n"; got != want {
		t.Errorf("secrets = %q, ожидали %q", got, want)
	}
}

func TestSecretsUnsafeFileType(t *testing.T) {
	t.Parallel()
	d, l := newDomain(t)
	target := filepath.Join(t.TempDir(), "elsewhere")
	writeFile(t, target, "TOKEN=x\n", 0o600)
	if err := os.MkdirAll(l.EtcDir(testExt), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, l.SecretsFile(testExt)); err != nil {
		t.Fatal(err)
	}
	_, err := d.Secrets()
	wantCerr(t, err, cerr.ExitPerm, "perm.secrets")
	_, err = d.Set([]Change{{"TOKEN", "y"}}, SetOpts{})
	wantCerr(t, err, cerr.ExitPerm, "perm.secrets")
}

func TestShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		config  string
		secrets string // "-" — файла нет
		secPerm os.FileMode
		want    map[string]Entry
	}{
		{
			name:    "только умолчания",
			secrets: "-",
			want: map[string]Entry{
				"PORT":    {Key: "PORT", Value: "8080", Origin: "default", Type: "port"},
				"HOST":    {Key: "HOST", Value: "", Origin: "default", Type: "host"},
				"TOKEN":   {Key: "TOKEN", Value: "(пусто)", Origin: "default", Type: "str", Secret: true},
				"VERSION": {Key: "VERSION", Value: "1.0", Origin: "default", Type: "str"},
			},
		},
		{
			name:    "значения из файла, маска и invalid",
			config:  "PORT=99999\nMODE=b\nLABEL_env=prod\nSTRAY=1\n",
			secrets: "TOKEN=hunter2\n",
			want: map[string]Entry{
				"PORT":      {Key: "PORT", Value: "99999", Origin: "file", Type: "port", Invalid: true},
				"MODE":      {Key: "MODE", Value: "b", Origin: "file", Type: "enum:a,b"},
				"LABEL_env": {Key: "LABEL_env", Value: "prod", Origin: "file", Type: "str"},
				"STRAY":     {Key: "STRAY", Value: "1", Origin: "file", Invalid: true},
				"TOKEN":     {Key: "TOKEN", Value: "(задан)", Origin: "file", Type: "str", Secret: true},
			},
		},
		{
			name:    "пустой секрет в файле",
			secrets: "TOKEN=\n",
			want: map[string]Entry{
				"TOKEN": {Key: "TOKEN", Value: "(пусто)", Origin: "file", Type: "str", Secret: true},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, l := newDomain(t)
			if tc.config != "" {
				writeFile(t, l.ConfigFile(testExt), tc.config, 0o644)
			}
			if tc.secrets != "-" {
				writeFile(t, l.SecretsFile(testExt), tc.secrets, 0o600)
			}
			entries, err := d.Show()
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]Entry{}
			var order []string
			for _, e := range entries {
				got[e.Key] = e
				order = append(order, e.Key)
				if strings.Contains(e.Value, "hunter2") {
					t.Errorf("секрет в выводе: %+v", e)
				}
			}
			for k, w := range tc.want {
				if got[k] != w {
					t.Errorf("%s: %+v, ожидали %+v", k, got[k], w)
				}
			}
			if _, ok := got["LABEL_*"]; ok {
				t.Error("шаблон не должен быть строкой show")
			}
			for i := 1; i < len(order); i++ {
				if order[i-1] >= order[i] {
					t.Fatalf("порядок не по имени: %q", order)
				}
			}
		})
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		key      string
		secret   bool
		want     string
		wantExit int
		wantCode string
	}{
		{name: "из файла", key: "PORT", want: "9000"},
		{name: "умолчание", key: "MODE", want: "a"},
		{name: "шаблон из файла", key: "LABEL_env", want: "prod"},
		{name: "шаблон без значения", key: "LABEL_none", want: ""},
		{name: "неизвестный", key: "NOPE", wantExit: cerr.ExitNotFound, wantCode: "not_found.key"},
		{name: "секрет без флага", key: "TOKEN", wantExit: cerr.ExitUsage, wantCode: "usage.secret"},
		{name: "секрет с флагом", key: "TOKEN", secret: true, want: "hunter2"},
	}
	d, l := newDomain(t)
	writeFile(t, l.ConfigFile(testExt), "PORT=9000\nLABEL_env=prod\n", 0o644)
	writeFile(t, l.SecretsFile(testExt), "TOKEN=hunter2\n", 0o600)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := d.Get(tc.key, tc.secret)
			wantCerr(t, err, tc.wantExit, tc.wantCode)
			if got != tc.want {
				t.Errorf("Get(%s) = %q, ожидали %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestValuesAndSecrets(t *testing.T) {
	t.Parallel()
	d, l := newDomain(t)
	writeFile(t, l.ConfigFile(testExt), "PORT=9000\nLABEL_env=prod\nSTRAY=1\nTOKEN=misplaced\n", 0o644)
	writeFile(t, l.SecretsFile(testExt), "TOKEN=hunter2\n", 0o600)
	vals, err := d.Values()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"PORT": "9000", "HOST": "", "MODE": "a", "FLAG": "0", "RO": "x", "VERSION": "1.0", "LABEL_env": "prod"}
	if !reflect.DeepEqual(vals, want) {
		t.Errorf("Values() = %v, ожидали %v", vals, want)
	}
	sec, err := d.Secrets()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"hunter2", "misplaced"}; !reflect.DeepEqual(sec, want) {
		t.Errorf("Secrets() = %q, ожидали %q", sec, want)
	}
}

// Без доступа к secrets (не root): show маскирует «***», get и Secrets — 77.
func TestSecretsUnreadable(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root читает файл 0000")
	}
	d, l := newDomain(t)
	writeFile(t, l.SecretsFile(testExt), "TOKEN=hunter2\n", 0o000)
	entries, err := d.Show()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Key == "TOKEN" && (e.Value != MaskUnknown || e.Origin != OriginFile) {
			t.Errorf("TOKEN: %+v", e)
		}
	}
	_, err = d.Get("TOKEN", true)
	wantCerr(t, err, cerr.ExitPerm, "perm.root")
	_, err = d.Secrets()
	wantCerr(t, err, cerr.ExitPerm, "perm.root")
	if v, err := d.Get("PORT", false); err != nil || v != "8080" {
		t.Errorf("Get(PORT) = %q, %v", v, err)
	}
}

// secrets есть, но не читается: Set не может сохранить чужие секреты —
// 77 perm.root, файлы не трогаются.
func TestSetSecretsUnreadable(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root читает файл 0000")
	}
	schema := testSchema(t)
	schema["PASS"] = keytype.Spec{Type: mustType(t, "str"), Empty: true, Secret: keytype.SecretFile, Description: "пароль"}
	l := paths.Layout{Root: t.TempDir()}
	writeFile(t, l.SecretsFile(testExt), "PASS=keep\n", 0o000)
	d := Open(l, testExt, schema)
	_, err := d.Set([]Change{{"PORT", "9090"}, {"TOKEN", "t"}}, SetOpts{})
	wantCerr(t, err, cerr.ExitPerm, "perm.root")
	if err := os.Chmod(l.SecretsFile(testExt), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := readFile(t, l.SecretsFile(testExt)); got != "PASS=keep\n" {
		t.Errorf("secrets = %q, ожидали нетронутый PASS=keep", got)
	}
	if _, ok := readFile(t, l.ConfigFile(testExt)); ok {
		t.Error("config создан, хотя Set отказал")
	}
}

// trustedDomain — домен в режиме настоящего корня (§13) с подменённым
// открытием: владелец — текущий uid (тест не может chown), opener
// записывает, какой файл и с каким режимом проверялся.
func trustedDomain(t *testing.T, opener func(path string, uid int, mode os.FileMode) (*os.File, error)) (*Domain, paths.Layout) {
	t.Helper()
	d, l := newDomain(t)
	d.trusted, d.uid, d.openTrusted = true, os.Getuid(), opener
	return d, l
}

// На настоящем корне config читается через fsx.OpenTrusted с режимом 0644.
func TestTrustedReadsConfig(t *testing.T) {
	t.Parallel()
	opened := map[string]os.FileMode{}
	var mu sync.Mutex
	d, l := trustedDomain(t, func(path string, uid int, mode os.FileMode) (*os.File, error) {
		mu.Lock()
		opened[path] = mode
		mu.Unlock()
		if uid != os.Getuid() {
			t.Errorf("uid %d", uid)
		}
		return openPlain(path)
	})
	writeFile(t, l.ConfigFile(testExt), "PORT=1\n", 0o644)
	writeFile(t, l.SecretsFile(testExt), "TOKEN=s\n", 0o600)
	if _, err := d.Show(); err != nil {
		t.Fatal(err)
	}
	if opened[l.ConfigFile(testExt)] != 0o644 || opened[l.SecretsFile(testExt)] != 0o600 {
		t.Errorf("проверенные открытия %v", opened)
	}
	// Отказ проверки — 77 perm.config.
	d2, l2 := trustedDomain(t, func(path string, _ int, _ os.FileMode) (*os.File, error) {
		return nil, fmt.Errorf("%s: %w", path, fsx.ErrUnsafePerm)
	})
	writeFile(t, l2.ConfigFile(testExt), "PORT=1\n", 0o666)
	_, err := d2.Show()
	wantCerr(t, err, cerr.ExitPerm, "perm.config")
	_, err = d2.Set([]Change{{"PORT", "2"}}, SetOpts{})
	wantCerr(t, err, cerr.ExitPerm, "perm.config")
}

// Перед записью на настоящем корне каталог настроек расширения обязан
// принадлежать root и не быть доступным на запись группе и всем — иначе 77.
func TestTrustedEtcDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mode     os.FileMode
		link     bool
		wantExit int
	}{
		{name: "0755", mode: 0o755},
		{name: "запись группе", mode: 0o775, wantExit: cerr.ExitPerm},
		{name: "запись всем", mode: 0o777, wantExit: cerr.ExitPerm},
		{name: "ссылка", mode: 0o755, link: true, wantExit: cerr.ExitPerm},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, l := trustedDomain(t, func(path string, _ int, _ os.FileMode) (*os.File, error) { return openPlain(path) })
			dir := l.EtcDir(testExt)
			if tc.link {
				real := filepath.Join(t.TempDir(), "real")
				if err := os.Mkdir(real, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, dir); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, tc.mode); err != nil {
					t.Fatal(err)
				}
			}
			_, err := d.Set([]Change{{"PORT", "9090"}, {"TOKEN", "t"}}, SetOpts{})
			if tc.wantExit == 0 {
				wantCerr(t, err, 0, "")
				return
			}
			wantCerr(t, err, tc.wantExit, "perm.etc_dir")
			if _, ok := readFile(t, l.ConfigFile(testExt)); ok {
				t.Error("config записан в небезопасный каталог")
			}
		})
	}
}
