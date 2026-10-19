package manifest

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
)

// base — минимальный валидный манифест; строки таблицы правят его через mk.
const base = `schema: cream/v1
name: demo
version: 1.0.0
summary: демо-расширение
cream: ">=1.0.0"
platform: {family: deb, arch: [amd64, arm64]}
hooks: {install: {}}
`

// mk заменяет пары old→new в base (каждая old обязана найтись) и дописывает extra.
func mk(t *testing.T, extra string, pairs ...string) string {
	t.Helper()
	s := base
	for i := 0; i+1 < len(pairs); i += 2 {
		if !strings.Contains(s, pairs[i]) {
			t.Fatalf("mk: в base нет %q", pairs[i])
		}
		s = strings.Replace(s, pairs[i], pairs[i+1], 1)
	}
	return s + extra
}

// creamV1 — исполнитель 1.0.0.
func creamV1(t *testing.T) CreamBuild {
	t.Helper()
	b := ParseCreamBuild("1.0.0")
	if b.Dev {
		t.Fatal("1.0.0 разобран как dev")
	}
	return b
}

func TestParseRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		yaml     string
		wantExit int    // 0 — разбор успешен
		wantCode string // error.code при отказе
	}{
		// schema
		{name: "базовый манифест", yaml: base},
		{name: "нет schema", yaml: strings.Replace(base, "schema: cream/v1\n", "", 1), wantExit: 65, wantCode: "manifest.schema"},
		{name: "чужая схема", yaml: strings.Replace(base, "cream/v1", "cream/v2", 1), wantExit: 65, wantCode: "manifest.schema"},
		{name: "layout 2", yaml: "name: demo\nversion: 1.0.0\nlayout: 2\n", wantExit: 65, wantCode: "manifest.schema"},
		{name: "пустой файл", yaml: "", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "не отображение", yaml: "- a\n- b\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "битый YAML", yaml: "schema: [\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "два документа", yaml: base + "---\nname: other\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "дубль ключа", yaml: base + "name: again\n", wantExit: 65, wantCode: "manifest.invalid"},

		// name
		{name: "имя с точкой и дефисом", yaml: mk(t, "", "name: demo", "name: a.b-c_1")},
		{name: "имя из одного знака", yaml: mk(t, "", "name: demo", "name: a")},
		{name: "имя 64 знака", yaml: mk(t, "", "name: demo", "name: a"+strings.Repeat("b", 63))},
		{name: "имя 65 знаков", yaml: mk(t, "", "name: demo", "name: a"+strings.Repeat("b", 64)), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя с заглавной", yaml: mk(t, "", "name: demo", "name: Demo"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя с _ в начале", yaml: mk(t, "", "name: demo", "name: _demo"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя со слешем", yaml: mk(t, "", "name: demo", "name: a/b"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "нет имени", yaml: mk(t, "", "name: demo\n", ""), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя cream", yaml: mk(t, "", "name: demo", "name: cream"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя lib", yaml: mk(t, "", "name: demo", "name: lib"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя ext", yaml: mk(t, "", "name: demo", "name: ext"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя installed", yaml: mk(t, "", "name: demo", "name: installed"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя lock", yaml: mk(t, "", "name: demo", "name: lock"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "имя global (Р15)", yaml: mk(t, "", "name: demo", "name: global"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "префикс cream- (Р15)", yaml: mk(t, "", "name: demo", "name: cream-x"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "creamy — не префикс cream-", yaml: mk(t, "", "name: demo", "name: creamy")},

		// version
		{name: "версия pre-release", yaml: mk(t, "", "version: 1.0.0", "version: 1.0.0-rc.1")},
		{name: "версия из двух чисел", yaml: mk(t, "", "version: 1.0.0", "version: \"1.0\""), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "версия с v", yaml: mk(t, "", "version: 1.0.0", "version: v1.0.0"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "нет версии", yaml: mk(t, "", "version: 1.0.0\n", ""), wantExit: 65, wantCode: "manifest.invalid"},

		// summary
		{name: "summary 120 знаков кириллицей", yaml: mk(t, "", "summary: демо-расширение", "summary: "+strings.Repeat("я", 120))},
		{name: "summary 121 знак", yaml: mk(t, "", "summary: демо-расширение", "summary: "+strings.Repeat("я", 121)), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "нет summary", yaml: mk(t, "", "summary: демо-расширение\n", ""), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "summary в две строки", yaml: mk(t, "", "summary: демо-расширение", "summary: \"a\\nb\""), wantExit: 65, wantCode: "manifest.invalid"},

		// cream
		{name: "cream — ровно версия исполнителя", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: "=1.0.0"`)},
		{name: "cream с верхней границей мажора", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: ">=1.0.0 <2"`)},
		{name: "cream старее исполнителя", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: ">=1.2.0"`), wantExit: 68, wantCode: "platform.cream_version"},
		{name: "cream чужой мажор", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: ">=2.0.0"`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "cream нулевой мажор", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: ">=0.9.0"`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "cream через два мажора", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: ">=1.0.0 <3"`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "cream без нижней границы", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: "<2"`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "cream не диапазон", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: "1.0.0"`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "нет cream", yaml: mk(t, "", "cream: \">=1.0.0\"\n", ""), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "cream важнее неизвестного поля", yaml: mk(t, "future: 1\n", `cream: ">=1.0.0"`, `cream: ">=1.5.0"`), wantExit: 68, wantCode: "platform.cream_version"},

		// kind
		{name: "kind extension явно", yaml: base + "kind: extension\n"},
		{name: "kind library без хуков", yaml: mk(t, "kind: library\n", "hooks: {install: {}}\n", "")},
		{name: "kind library с хуками", yaml: base + "kind: library\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "kind чужой", yaml: base + "kind: plugin\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "extension без install", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {remove: {}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "extension без hooks", yaml: mk(t, "", "hooks: {install: {}}\n", ""), wantExit: 65, wantCode: "manifest.invalid"},

		// platform
		{name: "family не deb", yaml: mk(t, "", "family: deb", "family: rpm"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "нет family", yaml: mk(t, "", "family: deb, ", ""), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "нет platform", yaml: mk(t, "", "platform: {family: deb, arch: [amd64, arm64]}\n", ""), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "arch aarch64 и x86_64", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: [x86_64, aarch64]")},
		{name: "arch пустой", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: []"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "arch нет", yaml: mk(t, "", ", arch: [amd64, arm64]", ""), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "arch armhf", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: [armhf]"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "arch дубль после канонизации", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: [amd64, x86_64]"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "arch скаляр", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: amd64"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "os с диапазоном", yaml: mk(t, "", "arch: [amd64, arm64]", `arch: [amd64], os: [{id: debian, version: ">=12"}, {id: ubuntu, version: ">=22.04"}]`)},
		{name: "os без версии", yaml: mk(t, "", "arch: [amd64, arm64]", `arch: [amd64], os: [{id: debian}]`)},
		{name: "os голая версия", yaml: mk(t, "", "arch: [amd64, arm64]", `arch: [amd64], os: [{id: debian, version: "12"}]`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "os без id", yaml: mk(t, "", "arch: [amd64, arm64]", `arch: [amd64], os: [{version: ">=12"}]`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "os id с заглавной", yaml: mk(t, "", "arch: [amd64, arm64]", `arch: [amd64], os: [{id: Debian}]`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "os неизвестное поле", yaml: mk(t, "", "arch: [amd64, arm64]", `arch: [amd64], os: [{id: debian, codename: bookworm}]`), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "commands", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: [amd64], commands: [curl, tar]")},
		{name: "commands с путём", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: [amd64], commands: [/usr/bin/curl]"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "commands с пробелом", yaml: mk(t, "", "arch: [amd64, arm64]", `arch: [amd64], commands: ["curl -s"]`), wantExit: 65, wantCode: "manifest.invalid"},

		// targets
		{name: "targets build none", yaml: base + "targets: {build: none}\n"},
		{name: "targets build files — зарезервировано", yaml: base + "targets: {build: files}\n", wantExit: 65, wantCode: "manifest.reserved"},
		{name: "targets build чужое", yaml: base + "targets: {build: image}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "live confirm с note", yaml: base + "targets: {live: confirm, live_note: снесёт docker.io}\n"},
		{name: "live confirm без note", yaml: base + "targets: {live: confirm}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "live no без note", yaml: base + "targets: {live: \"no\"}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "live no без кавычек с note", yaml: base + "targets: {live: no, live_note: только образ}\n"},
		{name: "live yes без кавычек", yaml: base + "targets: {live: yes}\n"},
		{name: "live чужое", yaml: base + "targets: {live: maybe, live_note: x}\n", wantExit: 65, wantCode: "manifest.invalid"},

		// provides / requires / conflicts
		{name: "provides токены", yaml: base + "provides: [container-runtime, a.b_c]\n"},
		{name: "provides с заглавной", yaml: base + "provides: [Runtime]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "provides дубль", yaml: base + "provides: [a, a]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "provides скаляр", yaml: base + "provides: runtime\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "requires пустой токен", yaml: base + "requires: [\"\"]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "requires скаляр", yaml: base + "requires: runtime\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "conflicts оба вида", yaml: base + "provides: [desktop-session]\nconflicts: {extensions: [gnome], capabilities: [desktop-session]}\n"},
		{name: "conflicts с собой по имени", yaml: base + "conflicts: {extensions: [demo]}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "conflicts имя неверное", yaml: base + "conflicts: {extensions: [Gnome]}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "conflicts список вместо объекта", yaml: base + "conflicts: [gnome]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "conflicts неизвестное поле", yaml: base + "conflicts: {names: [gnome]}\n", wantExit: 65, wantCode: "manifest.invalid"},

		// depends
		{name: "depends с диапазоном", yaml: base + "depends: [{name: teleport, version: \">=4.0.0 <5\"}, {name: cloud-user}]\n"},
		{name: "depends битый диапазон", yaml: base + "depends: [{name: teleport, version: \"4.x\"}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "depends без имени", yaml: base + "depends: [{version: \">=1\"}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "depends на себя", yaml: base + "depends: [{name: demo}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "depends дубль", yaml: base + "depends: [{name: a}, {name: a}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "depends строкой", yaml: base + "depends: [teleport]\n", wantExit: 65, wantCode: "manifest.invalid"},

		// hooks
		{name: "все хуки MVP", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {timeout: 20m}, upgrade: {}, remove: {}, apply: {timeout: 30s}, configure: {stage: system}}")},
		{name: "хук пустым значением", yaml: mk(t, "", "hooks: {install: {}}", "hooks:\n  install:\n  remove:")},
		{name: "configure без stage", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "configure stage graphical при cream >=1.0.0 — нужен 1.1.0", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {stage: graphical}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "configure stage early при cream >=1.0.0 — нужен 1.1.0", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {stage: early}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "configure stage cloud-user при cream >=1.0.0 — нужен 1.1.0", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {stage: cloud-user}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "configure stage чужая", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {stage: late}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "configure after/before/wants", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {stage: system, after: [docker.service], before: [getty.target], wants: [network-online.target]}}")},
		{name: "configure after с переводом строки", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {stage: system, after: [\"a.service\\nExecStart=/bin/x\"]}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "configure after без суффикса", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {stage: system, after: [docker]}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "configure after скаляр", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, configure: {stage: system, after: docker.service}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "stage у install", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {stage: system}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "hooks.secret — зарезервировано", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, secret: {}}"), wantExit: 65, wantCode: "manifest.reserved"},
		{name: "hooks.check — зарезервировано", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, check: {}}"), wantExit: 65, wantCode: "manifest.reserved"},
		{name: "хук чужой", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, postinst: {}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "timeout без единицы", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {timeout: 60}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "timeout нулевой", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {timeout: 0s}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "timeout отрицательный", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {timeout: -1m}}"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "хук неизвестное поле", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {retries: 3}}"), wantExit: 65, wantCode: "manifest.invalid"},

		// config.keys
		{name: "ключ со всеми полями", yaml: base + "config:\n  keys:\n    PORT: {type: port, default: \"8080\", description: порт, readonly: true, scope: install}\n"},
		{name: "ключ bool по умолчанию 0", yaml: base + "config: {keys: {ON: {type: bool, default: 0, description: вкл}}}\n"},
		{name: "ключ bool по умолчанию false", yaml: base + "config: {keys: {ON: {type: bool, default: false, description: вкл}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ без description", yaml: base + "config: {keys: {A: {type: str, default: x}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ без type", yaml: base + "config: {keys: {A: {default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ тип неизвестный", yaml: base + "config: {keys: {A: {type: float, default: \"1\", description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ пустое умолчание без empty", yaml: base + "config: {keys: {A: {type: str, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ пустое умолчание с empty", yaml: base + "config: {keys: {A: {type: host, default: \"\", empty: true, description: a}}}\n"},
		{name: "ключ умолчание против типа", yaml: base + "config: {keys: {A: {type: port, default: \"0\", description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ имя строчными", yaml: base + "config: {keys: {port: {type: port, default: \"1\", description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ CREAM_ занят", yaml: base + "config: {keys: {CREAM_X: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ LD_PRELOAD занят", yaml: base + "config: {keys: {LD_PRELOAD: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ LD_LIBRARY_PATH занят", yaml: base + "config: {keys: {LD_LIBRARY_PATH: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ BASH_ENV занят", yaml: base + "config: {keys: {BASH_ENV: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ ENV занят", yaml: base + "config: {keys: {ENV: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ IFS занят", yaml: base + "config: {keys: {IFS: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ PATH занят", yaml: base + "config: {keys: {PATH: {type: path, default: /bin, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "secret file", yaml: base + "config: {keys: {TOKEN: {type: str, default: \"\", empty: true, secret: file, description: a}}}\n"},
		{name: "secret hook — зарезервировано", yaml: base + "config: {keys: {TOKEN: {type: str, default: \"\", empty: true, secret: hook, description: a}}}\n", wantExit: 65, wantCode: "manifest.reserved"},
		{name: "secret чужой", yaml: base + "config: {keys: {TOKEN: {type: str, default: \"\", empty: true, secret: vault, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "secret true", yaml: base + "config: {keys: {TOKEN: {type: str, default: \"\", empty: true, secret: true, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "scope чужой", yaml: base + "config: {keys: {A: {type: str, default: x, scope: boot, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "шаблон ключа", yaml: base + "config: {keys: {LBL_*: {type: str, pattern: true, description: a}}}\n"},
		{name: "шаблон без pattern", yaml: base + "config: {keys: {LBL_*: {type: str, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "pattern без звёздочки", yaml: base + "config: {keys: {LBL: {type: str, default: x, pattern: true, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "шаблон с умолчанием", yaml: base + "config: {keys: {LBL_*: {type: str, default: x, pattern: true, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "config неизвестное поле", yaml: base + "config: {knobs: {}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ неизвестное поле", yaml: base + "config: {keys: {A: {type: str, default: x, description: a, ro: true}}}\n", wantExit: 65, wantCode: "manifest.invalid"},

		// actions
		{name: "действие пустое", yaml: base + "actions: {status: {}}\n"},
		{name: "действие имя с подчёркиванием", yaml: base + "actions: {set_managed: {}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "действие имя с точкой", yaml: base + "actions: {a.b: {}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "args pairs с params", yaml: base + "config: {keys: {PROXY: {type: host, default: \"\", empty: true, description: p}}}\nactions: {join: {args: pairs, params: {PROXY: {required: true}}}}\n"},
		{name: "args free", yaml: base + "actions: {set: {args: free}}\n"},
		{name: "параметр действия PATH занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {PATH: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия LD_PRELOAD занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {LD_PRELOAD: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия LD_LIBRARY_PATH занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {LD_LIBRARY_PATH: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия BASH_ENV занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {BASH_ENV: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия ENV занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {ENV: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия IFS занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {IFS: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия HOME занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {HOME: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия PWD занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {PWD: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия LANG занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {LANG: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "параметр действия CREAM_EXT занят (A10)", yaml: base + "actions: {join: {args: pairs, params: {CREAM_EXT: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ PATH занят", yaml: base + "config: {keys: {PATH: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ HOME занят", yaml: base + "config: {keys: {HOME: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ IFS занят", yaml: base + "config: {keys: {IFS: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "ключ BASH_ENV занят", yaml: base + "config: {keys: {BASH_ENV: {type: str, default: x, description: a}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "args чужой", yaml: base + "actions: {set: {args: all}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "params без pairs", yaml: base + "actions: {set: {args: free, params: {X: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "param без типа и без ключа", yaml: base + "actions: {join: {args: pairs, params: {X: {required: true}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "param тип по шаблону ключа", yaml: base + "config: {keys: {LBL_*: {type: str, pattern: true, description: a}}}\nactions: {join: {args: pairs, params: {LBL_ENV: {}}}}\n"},
		{name: "param тип расходится с ключом", yaml: base + "config: {keys: {P: {type: port, default: \"1\", description: a}}}\nactions: {join: {args: pairs, params: {P: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "param тип битый", yaml: base + "actions: {join: {args: pairs, params: {X: {type: \"re:(\"}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "param имя строчными", yaml: base + "actions: {join: {args: pairs, params: {x: {type: str}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "stdin secret с секретным param", yaml: base + "actions: {join: {args: pairs, params: {S: {type: str, secret: true}}, stdin: secret}}\n"},
		{name: "два секретных param", yaml: base + "actions: {join: {args: pairs, params: {S: {type: str, secret: true}, T: {type: str, secret: true}}, stdin: secret}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "stdin secret без секретного param", yaml: base + "actions: {join: {args: pairs, params: {S: {type: str}}, stdin: secret}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "секретный param без stdin secret", yaml: base + "actions: {join: {args: pairs, params: {S: {type: str, secret: true}}}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "stdin pass", yaml: base + "actions: {feed: {stdin: pass}}\n"},
		{name: "stdin чужой", yaml: base + "actions: {feed: {stdin: file}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "lock чужой", yaml: base + "actions: {a: {lock: global}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "output чужой", yaml: base + "actions: {a: {output: yaml}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "phases пустой", yaml: base + "actions: {a: {phases: []}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "phases чужая", yaml: base + "actions: {a: {phases: [boot]}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "phases дубль", yaml: base + "actions: {a: {phases: [live, live]}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "phases скаляр", yaml: base + "actions: {a: {phases: live}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "action timeout битый", yaml: base + "actions: {a: {timeout: soon}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "action summary длинный", yaml: base + "actions: {a: {summary: " + strings.Repeat("x", 121) + "}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "action неизвестное поле", yaml: base + "actions: {a: {exec: /bin/true}}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "действие у library", yaml: mk(t, "kind: library\nactions: {a: {}}\n", "hooks: {install: {}}\n", "")},

		// files
		{name: "files с режимом", yaml: base + "files: [{src: files/a.service, dst: /etc/systemd/system/a.service, mode: \"0644\"}]\n"},
		{name: "files режим без кавычек", yaml: base + "files: [{src: files/a, dst: /usr/local/bin/a, mode: 0755}]\n"},
		{name: "files без режима", yaml: base + "files: [{src: files/a, dst: /etc/a}]\n"},
		{name: "files в каталог чужого расширения (Р11)", yaml: base + "files: [{src: files/x, dst: /etc/cream/teleport/command-labels.d/x}]\n"},
		{name: "files dst относительный", yaml: base + "files: [{src: files/a, dst: etc/a}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files dst не чистый", yaml: base + "files: [{src: files/a, dst: /etc/../root/a}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files dst корень", yaml: base + "files: [{src: files/a, dst: /}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files имя с cream-", yaml: base + "files: [{src: files/a, dst: /etc/systemd/system/cream-configure@demo.service}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files каталог с cream-", yaml: base + "files: [{src: files/a, dst: /etc/cream-tools/a}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files cream- в середине пути", yaml: base + "files: [{src: files/a, dst: /etc/systemd/system/cream-configure@demo.service.d/x.conf}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files creamy — не префикс cream-", yaml: base + "files: [{src: files/a, dst: /etc/creamy/a}]\n"},
		{name: "files /usr/bin/cream", yaml: base + "files: [{src: files/a, dst: /usr/bin/cream}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files /usr/sbin/cream", yaml: base + "files: [{src: files/a, dst: /usr/sbin/cream}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files /bin/cream (usrmerge)", yaml: base + "files: [{src: files/a, dst: /bin/cream}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files /usr/local/bin/creamer", yaml: base + "files: [{src: files/a, dst: /usr/local/bin/creamer}]\n"},
		{name: "files в config домена", yaml: base + "files: [{src: files/a, dst: /etc/cream/demo/config}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files в secrets чужого домена", yaml: base + "files: [{src: files/a, dst: /etc/cream/teleport/secrets}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files в sources.yaml", yaml: base + "files: [{src: files/a, dst: /etc/cream/sources.yaml}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files в /var/lib/cream", yaml: base + "files: [{src: files/a, dst: /var/lib/cream/installed/x.json}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files в /opt/cream", yaml: base + "files: [{src: files/a, dst: /opt/cream/ext/other/x}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files src абсолютный", yaml: base + "files: [{src: /etc/passwd, dst: /etc/a}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files src наружу", yaml: base + "files: [{src: ../x, dst: /etc/a}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files src пустой", yaml: base + "files: [{dst: /etc/a}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files dst дубль", yaml: base + "files: [{src: files/a, dst: /etc/a}, {src: files/b, dst: /etc/a}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files режим всем на запись", yaml: base + "files: [{src: files/a, dst: /etc/a, mode: \"0666\"}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files режим setuid", yaml: base + "files: [{src: files/a, dst: /etc/a, mode: \"4755\"}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files режим не восьмеричный", yaml: base + "files: [{src: files/a, dst: /etc/a, mode: rw}]\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "files объект вместо списка", yaml: base + "files: {src: files/a, dst: /etc/a}\n", wantExit: 65, wantCode: "manifest.invalid"},

		// отложенное и неизвестное
		{name: "bin — зарезервировано", yaml: base + "bin: [{name: tool, target: files/tool}]\n", wantExit: 65, wantCode: "manifest.reserved"},
		{name: "неизвестное поле верхнего уровня", yaml: base + "phase: build\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "неизвестное поле в platform", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: [amd64], libc: glibc"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "неизвестное поле в targets", yaml: base + "targets: {firstboot: true}\n", wantExit: 65, wantCode: "manifest.invalid"},
		{name: "x- на верхнем уровне", yaml: base + "x-owner: ops\nx-meta: {a: [1, 2]}\n"},
		{name: "x- в platform", yaml: mk(t, "", "arch: [amd64, arm64]", "arch: [amd64], x-note: тест")},
		{name: "x- в хуке", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {x-retries: 3}}")},
		{name: "x- в ключе", yaml: base + "config: {keys: {A: {type: str, default: x, description: a, x-ui: slider}}}\n"},
		{name: "x- в действии", yaml: base + "actions: {a: {x-icon: star}}\n"},
		{name: "x- в элементе files", yaml: base + "files: [{src: files/a, dst: /etc/a, x-why: legacy}]\n"},
		{name: "x- в элементе depends", yaml: base + "depends: [{name: libx, x-since: \"2\"}]\n"},
		{name: "X- заглавными — не x-", yaml: base + "X-owner: ops\n", wantExit: 65, wantCode: "manifest.invalid"},
	}
	cream := creamV1(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, err := Parse([]byte(tc.yaml), cream, nil)
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatalf("Parse: %v\n%s", err, tc.yaml)
				}
				if m == nil {
					t.Fatal("Parse вернул nil без ошибки")
				}
				return
			}
			if err == nil {
				t.Fatalf("Parse без ошибки, ждали %d %s\n%s", tc.wantExit, tc.wantCode, tc.yaml)
			}
			if got := cerr.ExitCode(err); got != tc.wantExit {
				t.Errorf("код %d, ждали %d: %v", got, tc.wantExit, err)
			}
			if got := cerr.CodeOf(err); got != tc.wantCode {
				t.Errorf("error.code %q, ждали %q: %v", got, tc.wantCode, err)
			}
		})
	}
}

// TestParseStages11: стадии early, cloud-user, graphical (§6.4) появились в
// cream 1.1.0 — манифест обязан поднять cream: до ">=1.1.0" (§4.2), а cream 1.0
// отказывает такому манифесту по cream: (68), а не по стадии.
func TestParseStages11(t *testing.T) {
	t.Parallel()
	v11 := ParseCreamBuild("1.1.0")
	const cfg = "hooks: {install: {}, configure: {stage: %s, after: [a.service]}}"
	tests := []struct {
		name, stage, rng string
		cream            CreamBuild
		wantExit         int
		wantCode         string
	}{
		{name: "early 1.1", stage: "early", rng: ">=1.1.0", cream: v11},
		{name: "cloud-user 1.1", stage: "cloud-user", rng: ">=1.1.0 <2", cream: v11},
		{name: "graphical 1.1", stage: "graphical", rng: ">=1.2.0", cream: ParseCreamBuild("1.2.0")},
		{name: "graphical на dev-сборке", stage: "graphical", rng: ">=1.1.0", cream: ParseCreamBuild("dev")},
		{name: "system 1.0 на 1.1", stage: "system", rng: ">=1.0.0", cream: v11},
		{name: "early при >=1.0.0", stage: "early", rng: ">=1.0.0", cream: v11, wantExit: 65, wantCode: "manifest.invalid"},
		{name: "cloud-user при >=1.0.5", stage: "cloud-user", rng: ">=1.0.5", cream: v11, wantExit: 65, wantCode: "manifest.invalid"},
		{name: "graphical на dev при >=1.0.0", stage: "graphical", rng: ">=1.0.0", cream: ParseCreamBuild("dev"), wantExit: 65, wantCode: "manifest.invalid"},
		{name: "early на cream 1.0", stage: "early", rng: ">=1.1.0", cream: creamV1(t), wantExit: 68, wantCode: "platform.cream_version"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := mk(t, "", `cream: ">=1.0.0"`, `cream: "`+tc.rng+`"`, "hooks: {install: {}}", fmt.Sprintf(cfg, tc.stage))
			m, err := Parse([]byte(doc), tc.cream, &cerr.Warnings{})
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatalf("Parse: %v\n%s", err, doc)
				}
				hk := m.Hooks[HookConfigure]
				if string(hk.Stage) != tc.stage || !slices.Equal(hk.After, []string{"a.service"}) {
					t.Errorf("configure = %+v", hk)
				}
				return
			}
			if cerr.ExitCode(err) != tc.wantExit || cerr.CodeOf(err) != tc.wantCode {
				t.Fatalf("err = %v (exit %d, code %q), want %d %s", err, cerr.ExitCode(err), cerr.CodeOf(err),
					tc.wantExit, tc.wantCode)
			}
			msg := err.Error()
			switch tc.wantExit {
			case 65:
				if !strings.Contains(msg, "cream 1.1.0") || !strings.Contains(msg, `">=1.1.0"`) {
					t.Errorf("сообщение не называет нужную версию: %s", msg)
				}
			case 68:
				if !strings.Contains(msg, "требует cream >=1.1.0, а это cream 1.0.0") {
					t.Errorf("сообщение 68: %s", msg)
				}
			}
		})
	}
}

// TestParseErrorMentionsField: сообщение называет поле и строку, чтобы автор
// манифеста нашёл ошибку без отладки.
func TestParseErrorMentionsField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{name: "неизвестное поле с путём и строкой", yaml: base + "targets: {firstboot: true}\n", want: []string{"targets.firstboot", "строка 8"}},
		{name: "неизвестное поле в действии", yaml: base + "actions:\n  a:\n    exec: x\n", want: []string{"actions.a.exec", "строка 10"}},
		{name: "зарезервированный хук", yaml: mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, check: {}}"), want: []string{"hooks.check", "зарезервировано"}},
		{name: "умолчание bool", yaml: base + "config: {keys: {ON: {type: bool, default: true, description: вкл}}}\n", want: []string{"config.keys.ON.default", "0 или 1"}},
		{name: "cream старее", yaml: mk(t, "", `cream: ">=1.0.0"`, `cream: ">=1.3.0"`), want: []string{">=1.3.0", "1.0.0"}},
	}
	cream := creamV1(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tc.yaml), cream, nil)
			if err == nil {
				t.Fatal("ждали ошибку")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("в %q нет %q", err.Error(), w)
				}
			}
		})
	}
}

func TestParseValues(t *testing.T) {
	t.Parallel()
	const doc = `schema: cream/v1
name: teleport
version: 4.0.0-rc.1
summary: Узел Teleport
cream: ">=1.0.0"
platform:
  family: deb
  arch: [x86_64, aarch64]
  os: [{id: debian, version: ">=12"}, {id: ubuntu}]
  commands: [curl, tar]
targets: {live: confirm, live_note: риск}
provides: [teleport-node]
requires: [network]
depends: [{name: cloud-user, version: ">=1.0.0 <2"}, {name: libx}]
conflicts: {extensions: [teleport-agent], capabilities: [teleport-node]}
hooks:
  install: {timeout: 20m}
  upgrade: {}
  remove: {}
  apply: {}
  configure: {stage: system, after: [docker.service]}
config:
  keys:
    TELEPORT_PROXY: {type: host, default: "", empty: true, readonly: true, description: proxy}
    TELEPORT_VERSION: {type: "re:^[0-9.]+$", default: "18.10.0", scope: install, description: версия}
    TOKEN: {type: str, default: "", empty: true, secret: file, description: токен}
    LBL_*: {type: str, pattern: true, description: метки}
actions:
  join:
    args: pairs
    params:
      TELEPORT_PROXY: {required: true}
      TOKEN: {}
      LBL_ENV: {}
    stdin: secret
    output: json
    timeout: 5m
  enroll: {args: pairs, params: {SECRET: {type: str, secret: true}}, stdin: secret}
  render: {lock: none, phases: [build, live], root: false}
  status: {summary: состояние}
files:
  - {src: files/teleport.service, dst: /etc/systemd/system/teleport.service, mode: "0644"}
  - {src: files/tool, dst: /usr/local/bin/tool}
x-owner: ops
`
	m, err := Parse([]byte(doc), creamV1(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Schema != SchemaV1 || m.Name != "teleport" || m.Version.String() != "4.0.0-rc.1" || m.Summary != "Узел Teleport" {
		t.Errorf("шапка: %+v", m)
	}
	if m.Cream.String() != ">=1.0.0" {
		t.Errorf("cream %q", m.Cream.String())
	}
	if m.Kind != KindExtension {
		t.Errorf("kind %q", m.Kind)
	}
	if m.Platform.Family != "deb" || !slices.Equal(m.Platform.Arch, []string{"amd64", "arm64"}) ||
		!slices.Equal(m.Platform.Commands, []string{"curl", "tar"}) {
		t.Errorf("platform %+v", m.Platform)
	}
	if len(m.Platform.OS) != 2 || m.Platform.OS[0].ID != "debian" || m.Platform.OS[0].Version == nil ||
		m.Platform.OS[0].Version.String() != ">=12" || m.Platform.OS[1].Version != nil {
		t.Errorf("os %+v", m.Platform.OS)
	}
	if m.Targets.Build != BuildExec || m.Targets.Live != LiveConfirm || m.Targets.LiveNote != "риск" {
		t.Errorf("targets %+v", m.Targets)
	}
	if !slices.Equal(m.Provides, []string{"teleport-node"}) || !slices.Equal(m.Requires, []string{"network"}) {
		t.Errorf("provides/requires %v %v", m.Provides, m.Requires)
	}
	if len(m.Depends) != 2 || m.Depends[0].Name != "cloud-user" || m.Depends[0].Version.String() != ">=1.0.0 <2" ||
		m.Depends[1].Version != nil {
		t.Errorf("depends %+v", m.Depends)
	}
	if !slices.Equal(m.Conflicts.Extensions, []string{"teleport-agent"}) ||
		!slices.Equal(m.Conflicts.Capabilities, []string{"teleport-node"}) {
		t.Errorf("conflicts %+v", m.Conflicts)
	}

	// Таймауты хуков по умолчанию §6.1: install — нет; upgrade — как у install.
	wantHooks := map[HookName]time.Duration{
		HookInstall: 20 * time.Minute, HookUpgrade: 20 * time.Minute, HookRemove: 30 * time.Minute,
		HookApply: 60 * time.Second, HookConfigure: 15 * time.Minute,
	}
	if len(m.Hooks) != len(wantHooks) {
		t.Errorf("хуков %d, ждали %d", len(m.Hooks), len(wantHooks))
	}
	for h, d := range wantHooks {
		if got := m.Hooks[h].Timeout; got != d {
			t.Errorf("таймаут %s = %v, ждали %v", h, got, d)
		}
	}
	cfg := m.Hooks[HookConfigure]
	if cfg.Stage != StageSystem || !slices.Equal(cfg.After, []string{"docker.service"}) {
		t.Errorf("configure %+v", cfg)
	}

	px := m.Config["TELEPORT_PROXY"]
	if px.Type.Kind != "host" || !px.Empty || !px.Readonly || px.Scope != "config" || px.Description != "proxy" {
		t.Errorf("TELEPORT_PROXY %+v", px)
	}
	if v := m.Config["TELEPORT_VERSION"]; v.Scope != "install" || v.Default != "18.10.0" {
		t.Errorf("TELEPORT_VERSION %+v", v)
	}
	if v := m.Config["TOKEN"]; v.Secret != "file" {
		t.Errorf("TOKEN %+v", v)
	}
	if v := m.Config["LBL_*"]; !v.Pattern {
		t.Errorf("LBL_* %+v", v)
	}

	join := m.Actions["join"]
	if join.Args != ArgsPairs || join.Stdin != StdinSecret || join.Lock != LockExclusive || join.Output != OutputJSON ||
		join.Timeout != 5*time.Minute || !slices.Equal(join.Phases, []Phase{PhaseLive}) || !join.Root {
		t.Errorf("join %+v", join)
	}
	if p := join.Params["TELEPORT_PROXY"]; p.Type == nil || p.Type.Kind != "host" || !p.Required || p.Secret {
		t.Errorf("param TELEPORT_PROXY %+v", p)
	}
	if p := join.Params["TOKEN"]; p.Type == nil || !p.Secret {
		t.Errorf("param TOKEN (секрет из config.keys) %+v", p)
	}
	if p := m.Actions["enroll"].Params["SECRET"]; p.Type == nil || p.Type.Kind != "str" || !p.Secret || p.Required {
		t.Errorf("param SECRET %+v", p)
	}
	if p := join.Params["LBL_ENV"]; p.Type == nil || p.Type.Kind != "str" {
		t.Errorf("param LBL_ENV (по шаблону) %+v", p)
	}
	render := m.Actions["render"]
	if render.Lock != LockNone || !slices.Equal(render.Phases, []Phase{PhaseBuild, PhaseLive}) || render.Root {
		t.Errorf("render %+v", render)
	}
	// Умолчания действия §7.1.
	status := m.Actions["status"]
	if status.Summary != "состояние" || status.Args != ArgsNone || status.Stdin != StdinNone ||
		status.Lock != LockExclusive || status.Output != OutputText || status.Timeout != 2*time.Minute ||
		!slices.Equal(status.Phases, []Phase{PhaseLive}) || !status.Root || len(status.Params) != 0 {
		t.Errorf("status (умолчания) %+v", status)
	}

	wantFiles := []File{
		{Src: "files/teleport.service", Dst: "/etc/systemd/system/teleport.service", Mode: 0o644},
		{Src: "files/tool", Dst: "/usr/local/bin/tool", Mode: 0},
	}
	if !slices.Equal(m.Files, wantFiles) {
		t.Errorf("files %+v", m.Files)
	}
}

func TestParseDefaults(t *testing.T) {
	t.Parallel()
	m, err := Parse([]byte(base), creamV1(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindExtension || m.Targets.Build != BuildExec || m.Targets.Live != LiveYes {
		t.Errorf("умолчания kind/targets: %+v %+v", m.Kind, m.Targets)
	}
	if m.Hooks[HookInstall].Timeout != 0 {
		t.Errorf("install без таймаута по умолчанию, получили %v", m.Hooks[HookInstall].Timeout)
	}
	if m.Platform.OS != nil || len(m.Platform.Commands) != 0 || len(m.Provides) != 0 || len(m.Depends) != 0 ||
		len(m.Actions) != 0 || len(m.Files) != 0 || len(m.Config) != 0 {
		t.Errorf("пустые списки: %+v", m)
	}
	// upgrade без install-таймаута: тоже без таймаута.
	m2, err := Parse([]byte(mk(t, "", "hooks: {install: {}}", "hooks: {install: {}, upgrade: {}}")), creamV1(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Hooks[HookUpgrade].Timeout != 0 {
		t.Errorf("upgrade %v", m2.Hooks[HookUpgrade].Timeout)
	}
	// Свой таймаут upgrade не перекрывается install.
	m3, err := Parse([]byte(mk(t, "", "hooks: {install: {}}", "hooks: {install: {timeout: 1h}, upgrade: {timeout: 5m}}")), creamV1(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m3.Hooks[HookUpgrade].Timeout != 5*time.Minute {
		t.Errorf("upgrade %v", m3.Hooks[HookUpgrade].Timeout)
	}
}

// TestParseCreamBuild (Р18): предрелизная сборка сравнивается по основной
// версии; dev и не-semver удовлетворяют любому cream: с предупреждением.
func TestParseCreamBuild(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		build    string
		cream    string
		wantExit int
		wantWarn bool
	}{
		{name: "релиз выше", build: "1.1.0", cream: ">=1.0.0"},
		{name: "rc по основной версии", build: "1.0.0-rc.1", cream: ">=1.0.0"},
		{name: "префикс v", build: "v1.2.0", cream: ">=1.0.0"},
		{name: "rc ниже требования", build: "1.1.0-rc.1", cream: ">=1.2.0", wantExit: 68},
		{name: "релиз ниже требования", build: "1.1.0", cream: ">=1.2.0", wantExit: 68},
		{name: "dev", build: "dev", cream: ">=1.9.0", wantWarn: true},
		{name: "пусто — dev", build: "", cream: ">=1.0.0", wantWarn: true},
		{name: "не semver", build: "1.2.0+abc", cream: ">=1.0.0", wantWarn: true},
		{name: "dev не отменяет проверку диапазона", build: "dev", cream: ">=2.0.0", wantExit: 65},
		// Сборка из git describe не с тега — не релиз: dev, а не предрелиз 1.0.0.
		{name: "describe после тега — dev", build: "v1.0.0-3-g0d002ad", cream: ">=1.1.0", wantWarn: true},
		{name: "describe грязный — dev", build: "v1.0.0-3-g0d002ad-dirty", cream: ">=1.1.0", wantWarn: true},
		{name: "тег грязный — dev", build: "v1.0.0-dirty", cream: ">=1.1.0", wantWarn: true},
		{name: "rc с номером — не describe", build: "1.1.0-rc-3", cream: ">=1.2.0", wantExit: 68},
		// cream 1.1.1: describe после rc-тега — тоже dev, а не предрелиз.
		{name: "describe после rc — dev", build: "v1.1.0-rc.1-2-gabc1234", cream: ">=1.2.0", wantWarn: true},
		{name: "describe после rc грязный — dev", build: "1.1.0-rc.1-2-gabc1234-dirty", cream: ">=1.2.0", wantWarn: true},
		{name: "rc с хешеподобным словом — не describe", build: "1.1.0-rc.gabc1234", cream: ">=1.2.0", wantExit: 68},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := &cerr.Warnings{}
			yaml := mk(t, "", `cream: ">=1.0.0"`, `cream: "`+tc.cream+`"`)
			_, err := Parse([]byte(yaml), ParseCreamBuild(tc.build), w)
			if got := cerr.ExitCode(err); got != tc.wantExit {
				t.Fatalf("код %d, ждали %d: %v", got, tc.wantExit, err)
			}
			ws := w.List()
			if tc.wantWarn != (len(ws) == 1 && ws[0].Code == "platform.dev_build") || (!tc.wantWarn && len(ws) != 0) {
				t.Errorf("предупреждения %+v, ждали dev_build=%v", ws, tc.wantWarn)
			}
		})
	}
}

func TestCanonArch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"amd64", "amd64", true}, {"x86_64", "amd64", true},
		{"arm64", "arm64", true}, {"aarch64", "arm64", true},
		{"armhf", "", false}, {"AMD64", "", false}, {"", "", false},
	}
	for _, tc := range tests {
		got, ok := CanonArch(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("CanonArch(%q) = %q, %v; ждали %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in string
		ok bool
	}{
		{"teleport", true}, {"cloud-user", true}, {"a", true}, {"0x", true},
		{"", false}, {"-a", false}, {".a", false}, {"Teleport", false}, {"cream", false}, {"global", false},
		{"cream-configure", false}, {"x y", false},
	}
	for _, tc := range tests {
		if got := ValidName(tc.in); got != tc.ok {
			t.Errorf("ValidName(%q) = %v", tc.in, got)
		}
	}
}

func TestFileModeType(t *testing.T) {
	t.Parallel()
	m, err := Parse([]byte(base+"files: [{src: files/a, dst: /etc/a, mode: \"0750\"}]\n"), creamV1(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files[0].Mode != fs.FileMode(0o750) {
		t.Errorf("mode %o", m.Files[0].Mode)
	}
}

// TestParseEarlyLimits (cream 1.1.1): configure стадии early идёт до сети и до
// sysinit.target — загрузка ждёт его. Таймаут по умолчанию — 2m, больший
// зажимается до 2m с предупреждением manifest.early_timeout (манифест 1.1.0 не
// отвергается); after на network.target / network-online.target — цикл
// (early стоит Before=sysinit.target), предупреждение manifest.early_network.
func TestParseEarlyLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, hook  string
		wantTimeout time.Duration
		wantWarn    []string
	}{
		{"по умолчанию", "{stage: early}", 2 * time.Minute, nil},
		{"в пределе", "{stage: early, timeout: 90s}", 90 * time.Second, nil},
		{"ровно предел", "{stage: early, timeout: 2m}", 2 * time.Minute, nil},
		{"больше предела", "{stage: early, timeout: 10m}", 2 * time.Minute, []string{"manifest.early_timeout"}},
		{"after network-online", "{stage: early, after: [systemd-modules-load.service, network-online.target]}", 2 * time.Minute,
			[]string{"manifest.early_network"}},
		{"after network и таймаут", "{stage: early, timeout: 1h, after: [network.target]}", 2 * time.Minute,
			[]string{"manifest.early_timeout", "manifest.early_network"}},
		{"system не ограничен", "{stage: system, timeout: 1h, after: [network-online.target]}", time.Hour, nil},
		{"system по умолчанию", "{stage: system}", DefaultConfigureTimeout, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := mk(t, "", `cream: ">=1.0.0"`, `cream: ">=1.1.0"`, "hooks: {install: {}}",
				"hooks: {install: {}, configure: "+tc.hook+"}")
			var w cerr.Warnings
			m, err := Parse([]byte(doc), ParseCreamBuild("1.1.1"), &w)
			if err != nil {
				t.Fatalf("Parse: %v\n%s", err, doc)
			}
			if got := m.Hooks[HookConfigure].Timeout; got != tc.wantTimeout {
				t.Errorf("timeout = %s, want %s", got, tc.wantTimeout)
			}
			var codes []string
			for _, x := range w.List() {
				codes = append(codes, x.Code)
			}
			if !slices.Equal(codes, tc.wantWarn) {
				t.Errorf("предупреждения %v, ждали %v", codes, tc.wantWarn)
			}
		})
	}
}
