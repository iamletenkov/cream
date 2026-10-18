# План: cream MVP (`v1.0.0`)

> 2026-10-18. Спека — [docs/specs/2026-10-18-cream-core.md](../specs/2026-10-18-cream-core.md), объём — её §17 «MVP».
> Классификация — **архитектурная** (новая подсистема целиком). Цикл на задачу: TDD → `make lint` + `make test PKG="…"`
> → reviewer → коммит в ветке задачи → слияние контроллером в `feature/cream-mvp` → `make ci` после волны.
> Итог плана — PR `feature/cream-mvp` → `main` с футером `Release-As: 1.0.0`. План коммитит контроллер до волны 1
> вместе со строкой в `docs/README.md`.
>
> **Исполнителю:** после выполнения отметь свою задачу `- [x]` в Progress Tracker в том же коммите.

## Progress Tracker
<!-- Пункты разделены пустой строкой: параллельные отметки из разных worktree сливаются без конфликта. -->

- [ ] Task 1: Каркас CLI, коды ошибок, раскладка путей, зависимость yaml

- [ ] Task 2: semver-диапазоны и типы ключей (`semver`, `keytype`)

- [x] Task 3: EnvironmentFile и файловые примитивы (`envfile`, `fsx`)

- [ ] Task 4: Манифест `cream/v1`, `cream-catalog.yaml` и фикстуры §14 (`manifest`, `testdata/`)

- [ ] Task 5: Домен настроек (`config`)

- [ ] Task 6: Реестр, журнал и блокировки (`state`, `lock`)

- [ ] Task 7: Источники archive/path, синхронизация, `catalog pack` (`source`)

- [ ] Task 8: Запуск хуков и действий, фильтр секретов (`runner`)

- [ ] Task 9: systemd-стадии, `system init`, раскладка `files` (`systemd`, `place`)

- [ ] Task 10: Индекс и валидация каталога, поиск ссылки (`catalog`)

- [ ] Task 11: Резолвер: платформа, граф, порядок, lineage (`resolve`)

- [ ] Task 12: Ядро исполнителя: `set/show/get/list/config`, apply, `configure`, `system init` (`engine` core)

- [ ] Task 13: `install` и `upgrade` на живой машине (`engine/install`)

- [ ] Task 14: `remove` (`--purge`, `--forget`, `--force`) (`engine/remove`)

- [ ] Task 15: Действия: `cream run`, `cream actions` (`engine/action`)

- [ ] Task 16: Фаза build: `install --phase build --from-dir --record --params-file`, `policy-rc.d`

- [ ] Task 17: CLI источников и каталога: `source …`, `validate`, `catalog pack`, `resolve --json`

- [ ] Task 18: Упаковка `.deb` (nfpm), тарболы, воспроизводимость, `govulncheck`

- [ ] Task 19: Интеграция в контейнерах с systemd + пилотные фикстуры (`make test-integration`)

- [ ] Task 20: CI-матрица и сборка ассетов релиза (release-please)

- [ ] Task 21: Документация: `docs/cli.md`, ADR, README, карта AGENTS.md

- [ ] Task 22: Фаза build на живом `virt-customize` (`make test-build`)

- [ ] Task 23: Финальная интеграция + `make ci`

## Волны и владение файлами

До 3 исполнителей одновременно, каждый — в своём worktree от `feature/cream-mvp` (ветка `feature/cream-mvp-tN`).
Волна стартует, когда предыдущая слита в `feature/cream-mvp` и там зелёный `make ci` (контроллер, один прогон).

| Волна | Задачи (параллельно) | Общие файлы волны и их единственный владелец |
|---|---|---|
| 1 | T1, T2, T3 | `go.mod`, `go.sum`, `Makefile`, `cmd/cream/*`, `internal/cli/*` — **T1** |
| 2 | T4, T5, T6 | общих нет |
| 3 | T7, T8, T9 | общих нет |
| 4 | T10, T11, T12 | `internal/cli/wire.go` — **T12** |
| 5 | T13, T14, T15 | `internal/engine/engine.go`, `internal/engine/helpers_test.go`, `internal/cli/helpers_test.go` — никто (заморожены T12; нужна правка → стоп и доклад). Неэкспортируемые имена — с префиксом своей области: `install…`, `remove…`, `action…` (в том числе тестовые помощники) |
| 6 | T16, T17, T18 | `Makefile` — **T18**; `internal/cli/cmd_install.go` — **T16** |
| 7 | T19, T20, T21 | `Makefile` — **T19**; `.github/**` — **T20**; `docs/**`, `README.md`, `AGENTS.md` — **T21** |
| 8 | T22 | `Makefile` — T22 |
| 9 | T23 | всё (контроллер) |

Владелец каждого файла указан в задаче («Файлы»). Чужой файл нужен — остановиться и доложить контроллеру.
`go.mod`/`go.sum` после T1 не меняет никто: единственная внешняя зависимость — `go.yaml.in/yaml/v3` (§11.1).
`docs/STATUS.md` правит только контроллер между волнами и T23. Чекбокс своей задачи исполнитель отмечает в этом
плане **в коммите задачи**.

Правила исполнителю (вставлять в промпт): свои пути и список «не заходи»; `git add <пути>`, никогда `git add -A`;
коммит целыми файлами; Conventional Commits по-русски; тяжёлое (`make ci`, `make test-integration`,
`make test-build`) не запускать — это делает контроллер (исключения — T19 и T22, у них это и есть работа, под
замком `HEAVY_LOCK`).

## Контракты между пакетами (фиксируются до кода)

Граф импорта (стрелка — «импортирует»); циклов нет, нижние слои не знают о верхних:
```
cmd/cream → cli → engine → {catalog, resolve, source, config, state, lock, runner, systemd, place}
catalog → manifest, fsx, source(только тип SourceDir)   resolve → manifest, semver, state
manifest → semver, keytype                              config → envfile, keytype, fsx, paths
place → manifest, state, fsx, paths; systemd → fsx, paths  state, lock → fsx, paths
все → cerr                                              source → fsx, paths (валидатор — функцией)
```
Сигнатуры ниже — договор. Имена неэкспортируемого исполнитель выбирает сам; менять экспортируемое после слияния
волны — только через контроллера.

**`internal/cerr`** (T1)
```go
const (ExitOK=0; ExitCheck=1; ExitUsage=64; ExitData=65; ExitNotFound=66; ExitGraph=67; ExitNotApplicable=68
       ExitSource=69; ExitInternal=70; ExitState=74; ExitLock=75; ExitProtocol=76; ExitPerm=77; ExitHook=79)
type Error struct{ Exit int; Code, Message string; Details map[string]any; Err error } // Error(), Unwrap()
func New(exit int, code, format string, a ...any) *Error
func Wrap(err error, exit int, code, format string, a ...any) *Error
func ExitCode(err error) int          // nil→0, *Error (errors.As)→Exit, иначе 70
type Warning struct{ Code, Message string }
type Warnings struct{ list []Warning }   // Add(code, fmt, …), List() []Warning; передаётся явно
```
**`internal/paths`** (T1)
```go
type Layout struct{ Root string; User bool /* XDG-режим для sources/cache/lock-файла источников */ }
func Detect(env func(string) string, euid int, rootFlag, sourcesFlag, cacheFlag string) Layout
// Все методы возвращают путь под Root: Join(Root, "/etc/cream/…").
Bin() ExtTree(ext) ExtNew(ext) ExtPrev(ext) EtcDir(ext) ConfigFile(ext) SecretsFile(ext) StateDir(ext)
Sources() SourcesLock() CacheRoot() SourceCache(name) InstalledDir() InstalledFile(ext) History() LockDir()
GlobalLock() ExtLock(ext) Incoming(id) UnitTemplate() DropIn(ext) WantsDir(target) RunDir() RunReason(ext)
RebootRequired() PolicyRcD() OSRelease() SystemdRunning() /* /run/systemd/system */ CloudFinalUnit()
func (l Layout) NeedsRoot() bool      // Root == "/" — проверки root (77) только тогда (рулинг Р9)
```
**`internal/semver`** (T2)
```go
type Version struct{ Major, Minor, Patch uint64; Pre []string }
func Parse(s string) (Version, error); func ParseLoose(s string) (Version, error) // "12", "22.04" для os
func Compare(a, b Version) int; func (v Version) String() string
type Range struct{ /* конъюнкция компараторов */ }
func ParseRange(s string) (Range, error) // ">=1.0.0", ">=4.0.0 <5", "<5", "=1.2.3"; "||" — ошибка
func (r Range) Contains(v Version) bool; func (r Range) String() string
func (r Range) MajorBound() (lo, hi uint64, ok bool) // для «мажор диапазона = мажор схемы»
```
**`internal/keytype`** (T2)
```go
type Type struct{ Kind Kind; Enum []string; Re *regexp.Regexp; Elem *Type; Raw string }
func ParseType(s string) (Type, error)   // bool int port host path str enum:a,b re:<RE2> list:<тип>
func (t Type) Check(v string) error
type SecretMode string  // "", "file"; "hook" — распознаётся, но манифест отвергает (Р4)
type Scope string       // "config" | "install"
type Spec struct{ Type Type; Default string; Empty bool; Secret SecretMode; Readonly bool
                  Description string; Pattern bool; Scope Scope }
func (s Spec) CheckValue(v string) error // пустое только при Empty
type Schema map[string]Spec
func (s Schema) Lookup(key string) (Spec, bool) // точное имя, затем шаблоны PREFIX_*
```
**`internal/envfile`** (T3)
```go
type File struct{ /* строки с комментариями и порядком */ }
func Parse(data []byte) (*File, error)   // порт parse_env_file_internal() systemd
func (f *File) Get(k string) (string, bool); Set(k, v string); Delete(k string); Keys() []string; Bytes() []byte
func Quote(v string) string               // значение, которое Parse читает обратно байт в байт
```
**`internal/fsx`** (T3)
```go
func WriteAtomic(path string, data []byte, perm fs.FileMode) error // tmp в том же каталоге, fsync, rename, fsync dir
func CopyTree(src, dst string) error        // без следования ссылкам; ссылка наружу дерева — ошибка
func TreeDigest(root string) (string, error) // "sha256:<hex>" по §9.5 (F/L/D, режим 0644|0755)
func CheckOwnerPerm(path string, wantMode fs.FileMode) error // = OpenTrusted(path, 0, …) + Close
func OpenTrusted(path string, uid int, wantMode fs.FileMode) (*os.File, error) // openat+O_NOFOLLOW от /, каждый каталог — владелец uid, без записи группе/всем; читать через этот fd
func CheckOwnerPermFile(f *os.File, uid int, wantMode fs.FileMode) error // по открытому fd
// всё это → sentinel fsx.ErrUnsafePerm (у потребителя — cerr 77); CopyTree/TreeDigest — через os.Root
```
**`internal/manifest`** (T4)
```go
type Manifest struct{ Schema, Name string; Version semver.Version; Summary string; Cream semver.Range
  Kind Kind; Platform Platform; Targets Targets; Provides, Requires []string; Depends []Dep
  Conflicts Conflicts; Hooks map[HookName]Hook; Config keytype.Schema; Actions map[string]Action; Files []File }
type Platform struct{ Family string; Arch []string; OS []OSReq; Commands []string }
type OSReq struct{ ID string; Version *semver.Range }
type Targets struct{ Build BuildMode /*exec|none*/; Live LiveMode /*yes|confirm|no*/; LiveNote string }
type Dep struct{ Name string; Version *semver.Range }
type Conflicts struct{ Extensions, Capabilities []string }
type Hook struct{ Timeout time.Duration; Stage Stage; After, Before, Wants []string }
type Action struct{ Summary string; Args ArgsMode; Params map[string]Param; Stdin StdinMode; Lock LockMode
  Output OutputMode; Timeout time.Duration; Phases []Phase; Root bool }
type Param struct{ Type *keytype.Type; Required, Secret bool }
type File struct{ Src, Dst string; Mode fs.FileMode }
func Parse(data []byte, cream semver.Version) (*Manifest, error)
func Load(dir string, cream semver.Version) (*Manifest, error) // dir/extension.yaml
type Catalog struct{ Schema, Name string; Version semver.Version; Roots []string }
func ParseCatalog(data []byte) (*Catalog, error)
func CanonArch(s string) (string, bool)  // aarch64→arm64, x86_64→amd64
```
**`internal/config`** (T5)
```go
type Domain struct{ /* Layout, ext, Schema */ }
func Open(l paths.Layout, ext string, s keytype.Schema) *Domain
type Entry struct{ Key, Value, Origin /*default|file*/, Type string; Secret, Invalid bool }
func (d *Domain) Show() ([]Entry, error)                     // секреты: Value = "***"/"(задан)"/"(пусто)"
func (d *Domain) Get(key string, secret bool) (string, error)
type Change struct{ Key, Value string }
type SetOpts struct{ AllowReadonly, AllowInstallScope bool; Only []string /* install: только переданные */ }
func (d *Domain) Set(ch []Change, o SetOpts) (changed []string, err error) // всё проверено до записи; config+secrets
func (d *Domain) Values() (map[string]string, error)         // эффективные несекретные (умолчание ∪ файл)
func (d *Domain) Secrets() ([]string, error)                 // значения для фильтра вывода
func (d *Domain) ExportShell() (string, error)
type AssignInput struct{ Args []string; Stdin io.Reader; ParamsFile string; CheckPerm bool }
func ParseAssignments(in AssignInput, s keytype.Schema) ([]Change, error) // KEY=VAL, один KEY=-, секрет в argv→64
```
**`internal/state`** и **`internal/lock`** (T6)
```go
type SourceRef struct{ Name, Type, Origin, Resolved, CatalogVersion string }
type FileRec struct{ Dst, SHA256 string }
type ConfigureRec struct{ Stage, Target string }
type Record struct{ Schema, Name, Version, Kind, Status string; Source SourceRef; ExtDigest, ParamsDigest, Phase string
  InstalledAt, UpdatedAt time.Time; CreamVersion string; Files []FileRec; Bin []string
  Configure *ConfigureRec; RebootRequired bool; PrevVersion string `json:",omitempty"` }
type Store struct{ /* Layout, Now */ }
func NewStore(l paths.Layout, now func() time.Time) *Store
func (s *Store) Get(name string) (*Record, bool, error); List() ([]Record, error); Put(Record) error; Delete(name string) error
type HistoryEntry struct{ Time time.Time; Op, Ext, From, To, Status, Phase string }
func (s *Store) Append(HistoryEntry) error
func ParamsDigest(params map[string]string, secret func(key string) bool) string

type Mode int // Exclusive | Shared
type Locker struct{ /* Layout, Timeout, inherited *os.File из CREAM_LOCK_FD */ }
func NewLocker(l paths.Layout, timeout time.Duration, env func(string) string) *Locker
func (l *Locker) Global(ctx context.Context) (*Handle, error)
func (l *Locker) Ext(ctx context.Context, ext string, m Mode) (*Handle, error) // унаследованный fd того же файла → без ожидания
type Handle struct{ /* *os.File */ }; func (h *Handle) Release(); func (h *Handle) File() *os.File
```
**`internal/source`** (T7)
```go
type Source struct{ Name, Type, URL, SHA256, Path string } // type: archive|path; git → 65 source.reserved
type File struct{ Sources []Source }
func LoadFile(path string, checkPerm bool) (*File, error); func (f *File) Save(path string) error
func (f *File) Add(s Source, replace bool) (changed bool, err error); func (f *File) Remove(name string) error
type Fetcher interface{ Fetch(ctx context.Context, url string, w io.Writer, limit int64) error }
func NewHTTPFetcher(client *http.Client) Fetcher // https + file://, редиректы https→https ≤5
type LockEntry struct{ Name, Type, Origin, Resolved string; SyncedAt time.Time; CatalogVersion string }
type Syncer struct{ Layout paths.Layout; Fetch Fetcher; Validate func(dir string) (catalogVersion string, err error); Now func() time.Time }
func (s *Syncer) Sync(ctx context.Context, src Source) (LockEntry, error)
func ReadLock(path string) ([]LockEntry, error)
type SourceDir struct{ Source Source; Dir string; Synced bool; Lock *LockEntry }
func Dirs(l paths.Layout, f *File, lock []LockEntry) []SourceDir  // в порядке приоритета
func ExtractTarGz(r io.Reader, dst string) error   // враждебные архивы — отказ
func Pack(dir string, w io.Writer, epoch time.Time) error // детерминированный tar.gz
```
**`internal/runner`** (T8)
```go
type Spec struct{ Path string; Args []string; Dir string; Env []string; Stdin io.Reader; Stdout, Stderr io.Writer
  Timeout time.Duration; ExtraFiles []*os.File; Secrets []string }
type Result struct{ ExitCode int; TimedOut bool; Duration time.Duration }
type Runner interface{ Run(ctx context.Context, s Spec) (Result, error) }
type Exec struct{}                              // Setpgid, kill группы по таймауту, без shell
func NewSecretFilter(w io.Writer, secrets []string) io.WriteCloser // потоковая замена на "***", стыки чанков
type EnvInput struct{ Phase, Ext, ExtVersion, PrevVersion, ExtDir, EtcDir, StateDir, Root, ConfigFile,
  SecretsFile, Reason, Action, Output string; ChangedKeys []string; LockFD int; Values map[string]string }
func BuildEnv(in EnvInput) []string             // чистое окружение §6.3 + несекретные ключи
type Outcome int // OK | Reboot (10) | NotApplicable (20) | Failed
func Classify(code int) Outcome
```
**`internal/systemd`** и **`internal/place`** (T9)
```go
func UnitTemplate() []byte                      // go:embed cream-configure@.service
type StageSpec struct{ Stage string; After, Before, Wants []string }
func Enable(l paths.Layout, ext string, st StageSpec) (target string, err error) // drop-in + ссылка в *.wants
func Disable(l paths.Layout, ext string) error
func Init(l paths.Layout) error                 // каталоги §5 с режимами, шаблон юнита, пересчёт целей
type Systemctl interface{ Running(ctx context.Context) (state string, ok bool)
  DaemonReload(ctx context.Context) error; Restart(ctx context.Context, unit string, noBlock bool) error }
type ExecFunc func(ctx context.Context, name string, args ...string) (stdout []byte, code int, err error)
func NewSystemctl(l paths.Layout, run ExecFunc) Systemctl; type Fake struct{ … } // для тестов движка

type Owner func(dst string) (ext string, ok bool) // владелец по реестру
func Place(l paths.Layout, ext, tree string, files []manifest.File, owner Owner) ([]state.FileRec, error) // 67 при чужом dst
func Unplace(l paths.Layout, recs []state.FileRec, w *cerr.Warnings) error // изменённый → <dst>.cream-modified
```
**`internal/catalog`** (T10), **`internal/resolve`** (T11)
```go
type Entry struct{ Source source.SourceDir; Dir string; Manifest *manifest.Manifest; Digest string }
type Ref struct{ Source, Name string; Version *semver.Version }
func ParseRef(s string) (Ref, error)            // [source/]name[@version]
func Validate(root string, cream semver.Version) (Report, error) // §4.1 + предупреждения §6.3/§7.3/§8.4
func LoadExtension(dir string, cream semver.Version) (Entry, error) // для --from-dir
func Lookup(dirs []source.SourceDir, ref Ref, cream semver.Version, w *cerr.Warnings) (Entry, error)

type Node struct{ Manifest *manifest.Manifest; Installed bool; Files []string; Source state.SourceRef; Digest string }
type Host struct{ Phase, TargetArch, HostArch, Family string; OSID, OSVersion string
  HasCommand func(string) bool; AcceptRisk bool }
func DetectHost(l paths.Layout, phase string, lookPath func(string) (string, error)) (Host, error)
type Layer struct{ Name, Version, Source, SourceResolved, CatalogVersion, ExtDigest, ParamsDigest string
  Deps []string; Mode, Token string; Pinned bool; SecretParams []string } // §12.4; это же — record.json (Р6)
func Resolve(installed, requested []Node, params map[string]map[string]string, h Host, w *cerr.Warnings) ([]Layer, error)
func Dependents(installed []Node, name string) []string // для remove
```
**`internal/engine`** (T12 — ядро; T13–T16 — свои файлы)
```go
type Engine struct{ L paths.Layout; Store *state.Store; Locks *lock.Locker; Run runner.Runner
  Systemctl systemd.Systemctl; Now func() time.Time; Cream semver.Version; Warn *cerr.Warnings
  LookPath func(string) (string, error); Env func(string) string
  Stdin io.Reader; Stdout, Stderr io.Writer }
type Installed struct{ Record state.Record; Manifest *manifest.Manifest }
func (e *Engine) Installed() ([]Installed, error)
func (e *Engine) RunHook(ctx context.Context, in Installed, hook manifest.HookName, env runner.EnvInput) (runner.Outcome, error)
func (e *Engine) Set(ctx context.Context, ext string, ch []config.Change, o SetOptions) (SetResult, error)   // T12
func (e *Engine) Configure(ctx context.Context, ext, reason string, o ConfigureOptions) error              // T12
func (e *Engine) Install(ctx context.Context, r InstallRequest) (*InstallResult, error)                    // T13
func (e *Engine) Upgrade(ctx context.Context, names []string, all bool) ([]InstallResult, error)          // T13
func (e *Engine) Remove(ctx context.Context, name string, o RemoveOptions) (*RemoveResult, error)         // T14
func (e *Engine) RunAction(ctx context.Context, r ActionRequest) (ActionResult, error)                    // T15
func (e *Engine) InstallFromDir(ctx context.Context, r BuildRequest) (*InstallResult, error)              // T16
```
**`internal/cli`** (T1): `type App struct{ Stdin io.Reader; Stdout, Stderr io.Writer; Env func(string) string;
Euid func() int; Now func() time.Time }`; `func Run(args []string, app App) int`; `type Command struct{ Name, Usage
string; Flags func(*flag.FlagSet); Run func(*Context, []string) (result any, err error) }`; группы команд — методы
`sourceCommands`, `catalogCommands`, `configCommands`, `systemCommands`, `installCommands`, `removeCommands`,
`actionCommands`, `metaCommands`, каждая в своём файле `cmd_<group>.go`. T1 кладёт заглушки `return nil`; задача
группы заменяет файл целиком. Конверт `--json` (§11.4) и перевод `error → код` делает только `Run`.

## Рулинги планировщика (спека молчит; владельцу — на сведение)
- **Р1.** Отложенное из §17 в манифесте и `sources.yaml` → 65, `error.code` `manifest.reserved` / `source.reserved`,
  текст «зарезервировано в cream/v1, не реализовано в cream 1.0». Отложенный флаг CLI (`--with-deps`,
  `--allow-downgrade`, `--require-pinned`) → 64 `usage.reserved`. Отложенные команды (`search`, `info`, `verify`,
  `assume`, `lineage`, `check`, `status`, `path`) в диспетчере есть и отвечают 64 `usage.reserved`.
- **Р2.** `CREAM_LOCK_FD` передаёт только блокировку домена `lock/<ext>`. Вложенная операция, которой нужна
  `lock/global` (install/remove из хука), ждёт её и падает 75 — так и задумано.
- **Р3.** Стадии `early`, `cloud-user`, `graphical` — `manifest.reserved`; в `systemd` таблица стадий расширяемая,
  но заполнена только `system`.
- **Р4.** `secret: hook`, `hooks.secret`, `hooks.check`, `bin`, `targets.build: files`, `kind`-зависимые поля без
  поддержки — по Р1. `assumed.json` в MVP не читается.
- **Р5.** Фаза `cream run`: `CREAM_PHASE` из окружения (вложенный вызов из хука), иначе `live`.
- **Р6.** `record.json` фазы build = один элемент `resolve.Layer` (`resolve --json`, §12.4) плюс `source` из реестра;
  cream в госте сверяет `ext_digest` и `params_digest`.
- **Р7.** `umask 022` ставит `main` один раз на процесс (дочерние наследуют); других глобальных побочных эффектов нет.
- **Р8.** `cream upgrade <ext>` ищет новую версию в источнике из реестра; источника нет → 69 `source.missing`.
- **Р9.** Проверки «нужен root» (77) и прав файлов действуют только при `--root /`; с другим `--root` (тесты,
  фикстуры) они выключены, остальная логика та же.
- **Р10.** Тип `host`: DNS-имя (LDH, ASCII), IPv4 или IPv6, необязательный `:port`; IPv6 с портом — в `[]`.
  Так `TELEPORT_PROXY=proxy.example:443` проходит проверку без отдельного типа.
- **Р11.** Расширение A может через `files` класть файл в каталог настроек расширения B
  (`/etc/cream/<B>/command-labels.d/x`): владение — по реестру A (`FileRec`), конфликт 67 только если тот же `dst`
  уже записан в реестре другого расширения. `remove A` убирает только свой файл; `remove --purge B` удаляет каталог B
  целиком, запись A становится висячей, и `Unplace` A потом пропускает отсутствующий файл.
  Это нужно stvor-agent: drop-in командной метки в каталог `teleport`.

## Задачи

### Волна 1 — фундамент (T1 ∥ T2 ∥ T3)

#### Task 1: Каркас CLI, коды ошибок, раскладка путей, зависимость yaml
**Цель.** Диспетчер команд на stdlib `flag` с перестановкой аргументов (§11.1), глобальные флаги и переменные
окружения (§11.2), конверт `--json` (§11.4), коды выхода (§11.3), раскладка путей (§5) с `--root` и XDG-режимом.
Зафиксировать единственную зависимость.
**Файлы.** Изменить: `go.mod`, `go.sum` (`go.yaml.in/yaml/v3`, закреплённая версия; импортирует пока только
`internal/cli/deps.go` с `_`-импортом — удаляется в T4), `Makefile` (переменные `PKG ?= ./...`,
`TEST_P ?= 4` → `go test -p $(TEST_P)`, цель `test-golden` = тесты `./internal/cli` с `-update`; `HEAVY_LOCK ?=
/tmp/cream-heavy.lock` и обёртка `ci` в `flock $(HEAVY_LOCK)`), `cmd/cream/main.go` (только `umask` и
`cli.Run`), `cmd/cream/main_test.go`. Создать: `internal/cerr/{cerr.go,cerr_test.go}`,
`internal/paths/{paths.go,paths_test.go}`, `internal/cli/{run.go,args.go,envelope.go,commands.go,context.go,
cmd_meta.go,cmd_source.go,cmd_catalog.go,cmd_config.go,cmd_system.go,cmd_install.go,cmd_remove.go,cmd_action.go,
golden_test.go,run_test.go,args_test.go}`, `internal/cli/testdata/golden/version*.json`. `internal/version` не трогать.
**Тесты первыми.**
- `args_test`: таблица перестановки — `set teleport K=V --apply` ≡ `set --apply teleport K=V`; `--` прекращает
  разбор; для `run` всё после `<action>` не переставляется (`run --json ext act --json` → второй `--json` — аргумент).
- `run_test`: неизвестная команда → 64 `usage.unknown_command`; отложенная команда → 64 `usage.reserved`; ошибка
  `cerr` → её код; паника-свободный `error` → 70 `internal`; с `--json` stdout — ровно один объект, текст — в stderr.
- `golden_test`: `version --json` против `testdata/golden/version.json` (поле `contract: "cream/v1"`); хелпер
  `golden(t, name, got)` с флагом `-update` — им пользуются T12–T17.
- `paths_test`: таблица Detect — root/не-root, `--root`, `CREAM_ROOT`, `CREAM_SOURCES`, `CREAM_CACHE`, XDG; каждый
  метод под `Root=/tmp/x` даёт путь из §5.
- `cerr_test`: `ExitCode` по `errors.As` через обёртки.
**Готово.** `make lint` и `make test` зелёные; `make build && dist/cream version
--json` печатает конверт.
**Зависит от:** —.

#### Task 2: semver-диапазоны и типы ключей
**Цель.** Свой semver (~150 строк, §11.1) и типы ключей §8.2 с `Spec`/`Schema`.
**Файлы.** Создать: `internal/semver/{semver.go,range.go,semver_test.go,range_test.go}`,
`internal/keytype/{type.go,spec.go,type_test.go,spec_test.go}`.
**Тесты первыми.**
- semver: таблица Parse (валидные, `01.2.3`, пустой pre, `v1.2.3` — отказ), Compare с pre-release по semver 2.0;
  ParseLoose (`12`, `22.04`, `13`); ParseRange (`>=1.0.0`, `>=4.0.0 <5`, `<5`, `=1.2.3`, `||` → ошибка, мусор →
  ошибка); Contains — граничные; MajorBound.
- keytype: на каждый тип — строки «принять/отказать»: `port` 0/1/65535/65536, `host` по Р10 (имя LDH, IPv4, IPv6 в скобках при порте, `:port` 1–65535, IDN — отказ),
  `path` (абсолютный, без `\n`), `str` без управляющих, `$`, обратной кавычки; `enum:a,b`; `re:` с RE2 и
  отказ на синтаксисе вне RE2 (обратные ссылки); `list:re:…` через пробел, пустой список; `CheckValue` + `Empty`;
  `Schema.Lookup` для `PREFIX_*` (точное имя важнее шаблона).
**Готово.** `make test PKG="./internal/semver/... ./internal/keytype/..."` зелёный, `make lint` зелёный.
**Зависит от:** —.

#### Task 3: EnvironmentFile и файловые примитивы
**Цель.** Порт `parse_env_file_internal()` с сохранением комментариев/порядка и цитированием (§8.1); сверка с
systemd 249/252/255/257 — в T19; атомарная запись с инъекцией сбоя; копия дерева без ссылок наружу; отпечаток §9.5.
**Файлы.** Создать: `internal/envfile/{envfile.go,quote.go,envfile_test.go,reference_test.go}`,
`internal/envfile/testdata/cases/*.env`, `internal/fsx/{atomic.go,copytree.go,
digest.go,perm.go,*_test.go}`.
**Тесты первыми.**
- envfile: таблица разбора (кавычки, `\` в кавычках и без, продолжение строки, комментарии `#`/`;`, пустые,
  `export`-префикс — как в systemd); `reference_test` — каждый `cases/*.env` против
  `testdata/systemd-<ver>/`, а пока каталогов нет — `t.Skip("эталоны снимает T19")`; round-trip `Quote` → `Parse` (fuzz-подобная таблица из
  враждебных строк); `Set` сохраняет комментарии и порядок.
- fsx: `WriteAtomic` — инъекция сбоя между write и rename (неэкспортируемый хук теста) оставляет старый файл целым;
  `CopyTree` — бит исполнения сохранён, ссылка внутрь — ссылка, наружу/абсолютная — отказ, FIFO — отказ;
  `TreeDigest` — эталонный hex на фиксированном дереве, смена бита исполнения меняет отпечаток, порядок обхода не
  влияет; `CheckOwnerPerm` — 0666/0620 → 77.
**Готово.** `make test PKG="./internal/envfile/... ./internal/fsx/..."` и `make lint` зелёные.
**Зависит от:** — (импорт `cerr` из T1 не нужен: `CheckOwnerPerm` возвращает sentinel `fsx.ErrUnsafePerm`, перевод в
77 — у потребителя).

### Волна 2 — модели (T4 ∥ T5 ∥ T6)

#### Task 4: Манифест `cream/v1`, `cream-catalog.yaml` и фикстуры §14
**Цель.** Строгий разбор §4.2 и §7.1: `KnownFields`, игнор `x-*` (на `yaml.Node` вырезать ключи `x-` на всех уровнях
отображений до декодирования), скаляр вместо списка — отказ, отложенное — Р1/Р3/Р4, `cream:` против версии
исполнителя (68 `platform.cream_version`), мажор диапазона = 1.
**Файлы.** Создать: `internal/manifest/{types.go,parse.go,validate.go,catalog.go,parse_test.go,catalog_test.go,
fixtures_test.go}`, фикстуры (хуки — sh-скрипты с битом исполнения, пишут счётчик вызовов в `CREAM_STATE_DIR`):
`testdata/catalog/cream-catalog.yaml`, `testdata/catalog/extensions/<name>/…` для: `noop`, `with-config`,
`apply-hook`, `configure-system`, `action-json`, `action-bad-protocol`, `depends-lib`, `lib-fixture`,
`lock-restart`, `lock-restart-exclusive` (негативный вариант §14 п.7), `install-scope`, `conflict-name`,
`conflict-cap`, `requires-cap`, `files-mode`, `failing-install`, `reboot-required`, `not-applicable`,
`upgrade-fixture` (1.0.0), `live-confirm`; `testdata/catalog-next/…` (`upgrade-fixture` 2.0.0, `noop` 1.0.0);
`testdata/invalid/<случай>/` (имя каталога ≠ name, хук без бита исполнения, объявленный хук без файла и файл без
объявления, ссылка наружу, бинарь не ELF и без shebang, дубль имени в двух `roots`).
Удалить: `internal/cli/deps.go` (заглушка импорта из T1; в волне 2 других владельцев у `internal/cli` нет).
**Тесты первыми.** Одна большая таблица `{name, yaml, wantCode, wantErrCode}` — по строке на каждое правило §4.2:
неизвестная схема, имя (регэксп, зарезервированные `cream/lib/ext/installed/lock`), версия, summary > 120,
`cream:` старее исполнителя и чужой мажор, `kind: library` с хуками, `family` ≠ deb, arch-канонизация и пустой
список, `os` с диапазоном, `targets.build: files` → reserved, `live: confirm` без `live_note`, `provides` токены,
`depends` с диапазоном, конфликты, хук `configure` без `stage`, стадия `graphical` → reserved, `hooks.secret` →
reserved, `secret: hook` → reserved, `bin` → reserved, действие (имя, `args`, `stdin: secret` без секретного
параметра, `lock` по умолчанию `exclusive`, `phases` по умолчанию `[live]`, `timeout` по умолчанию 2m, `root`
по умолчанию true), `params` с типом из `config.keys`, `files` с `dst` вне абсолютного пути и `cream-`-префиксом
в **имени файла** → 65 (каталог `/etc/cream/<другое расширение>/…` в `dst` допустим — строка таблицы, Р11), неизвестное поле → 65, `x-foo` на любом уровне — игнор, скаляр вместо списка → 65. Таймауты хуков по
умолчанию §6.1. Каталог: схема, `roots`, версия. `fixtures_test`: каждое расширение `testdata/catalog` и
`testdata/catalog-next` грузится `manifest.Load` без ошибки; у `testdata/invalid/*` с манифестными дефектами —
ожидаемый код (дефекты дерева — имя каталога, биты, ссылки — ловит уже T10).
**Готово.** `make test PKG="./internal/manifest/..."` и `make lint` зелёные.
**Зависит от:** T1 (`cerr`, yaml в go.mod), T2.

#### Task 5: Домен настроек
**Цель.** `/etc/cream/<ext>/{config,secrets}` по §8.1–§8.3, §8.5: транзакция «всё проверено — потом запись»,
`readonly`, `scope: install`, секреты 0600, маска, `invalid`, `KEY=-`, `--params-file` (0600, root → иначе 77),
`ExportShell`.
**Файлы.** Создать: `internal/config/{domain.go,assign.go,export.go,domain_test.go,assign_test.go,export_test.go}`.
**Тесты первыми.**
- `Set`: таблица — валидный набор; один невалидный ключ → ничего не записано; `readonly` без/с `AllowReadonly`;
  `scope: install` без/с `AllowInstallScope` (`AllowReadonly` его не снимает); неизвестный ключ → 65; секретный
  ключ уходит в `secrets` 0600, в `config` его нет; комментарии оператора в `config` сохраняются; `changed`
  содержит только реально изменённые.
- `ParseAssignments`: `K=V`, два `K=-` → 64, секрет со значением в argv → 64 `secret.in_argv`, пустой stdin → 64,
  `--params-file` 0644 → 77 (при `CheckPerm`), формат env.
- `Show`: источник `default|file`, маска секрета, битое значение помечено `Invalid`.
- `ExportShell`: цитирование враждебных значений, секретов нет.
**Готово.** `make test PKG="./internal/config/..."`, `make lint` зелёные.
**Зависит от:** T1 (`paths`, `cerr`), T2 (`keytype`), T3 (`envfile`, `fsx`).

#### Task 6: Реестр, журнал и блокировки
**Цель.** `cream-state/v1` по файлу на расширение (§10), `history.jsonl`, `ParamsDigest` с литералом `<secret>`;
`flock` global/ext, shared/exclusive, таймаут → 75, наследование `CREAM_LOCK_FD` по (dev, ino) (Р2).
**Файлы.** Создать: `internal/state/{record.go,store.go,history.go,digest.go,*_test.go}`,
`internal/lock/{lock.go,lock_test.go}`.
**Тесты первыми.**
- state: golden JSON записи из §10; запись схемы `cream-state/v2` → 74 `state.newer`; `Put` атомарен (сбой между
  write и rename); `List` пропускает временные файлы; `Append` дописывает строку без параметров; `ParamsDigest` —
  таблица: порядок ключей не влияет, секрет → `KEY=<secret>`, значение секрета не влияет на дайджест.
- lock: две горутины — exclusive ждёт; shared ∥ shared; таймаут → 75 `lock.timeout`; `CREAM_LOCK_FD` на тот же
  файл — захват без ожидания; на чужой файл — игнор и обычное ожидание; проверка через дочерний процесс
  (`os/exec` теста с `ExtraFiles`).
**Готово.** `make test PKG="./internal/state/... ./internal/lock/..."`, `make lint` зелёные.
**Зависит от:** T1, T3.

### Волна 3 — ввод-вывод системы (T7 ∥ T8 ∥ T9)

#### Task 7: Источники archive/path, синхронизация, `catalog pack`
**Цель.** §9.1–§9.2 для `archive` и `path`; `git` → Р1. Сеть только в `Sync`. Детерминированный архив §11.2.
**Файлы.** Создать: `internal/source/{sources.go,fetch.go,extract.go,sync.go,lockfile.go,dirs.go,pack.go,*_test.go}`,
`internal/source/testdata/hostile/*.tar.gz` (генерируются тестом в `t.TempDir()`, не коммитятся бинарями — см. риск).
**Тесты первыми.**
- `sources_test`: Add идемпотентен (те же поля → `changed=false`), другие поля без `replace` → 65, голый путь
  вместо `file://` → 64, `type: git` → 65 `source.reserved`, `path` не абсолютный → 64; порядок = приоритет.
- `fetch_test` (`httptest.NewTLSServer`): редирект https→http → отказ, 6 редиректов → отказ, лимит 256 МиБ
  (параметризуемый), `file://`.
- `extract_test` (архивы собираются в тесте): абсолютный путь, `..`, симлинк наружу, hardlink наружу, устройство,
  FIFO → отказ; один верхний каталог снимается.
- `sync_test`: sha256 не совпал → 69 до распаковки; битый каталог (валидатор вернул ошибку) не подменяет прежний
  кеш; успешный — одним `rename`, `sources.lock` с `resolved` (`sha256:` архива, `sha256:` отпечатка path).
- `pack_test`: два прохода с одним `epoch` дают одинаковый sha256; uid/gid 0, сортировка, `gzip` без имени/mtime.
**Готово.** `make test PKG="./internal/source/..."`, `make lint` зелёные.
**Зависит от:** T1, T3.

#### Task 8: Запуск хуков и действий, фильтр секретов
**Цель.** §6.3 и §7.2: exec без оболочки, чистое окружение, cwd, stdin, таймаут с убийством группы процессов,
`ExtraFiles` для `CREAM_LOCK_FD`, классификация кодов 0/10/20, потоковый фильтр секретов.
**Файлы.** Создать: `internal/runner/{runner.go,env.go,filter.go,outcome.go,*_test.go}`,
`internal/runner/testdata/{echo-env,sleep,print-secret,exit-code}` (sh-скрипты с битом исполнения).
**Тесты первыми.**
- `BuildEnv`: golden-список переменных; секреты не попадают (значения нет в `Values` — проверка на входе);
  `PATH`/`LANG` фиксированы; порядок детерминирован.
- `Exec`: окружение процесса пусто, кроме выданного (скрипт `echo-env`); cwd; таймаут → `TimedOut` и дочерний
  `sleep` внука тоже убит (группа); stdin `/dev/null` по умолчанию; `ExtraFiles` даёт fd 3.
- `filter_test`: таблица — секрет целиком, на стыке двух `Write`, перекрывающиеся секреты (длинный раньше),
  пустой секрет игнорируется, `Close` сбрасывает хвост.
- `Classify`: 0, 10, 20, 1, 2, 137.
**Готово.** `make test PKG="./internal/runner/..."`, `make lint` зелёные.
**Зависит от:** T1.

#### Task 9: systemd-стадии, `system init`, раскладка `files`
**Цель.** Встроенный шаблон `cream-configure@.service` (§6.4), drop-in `10-stage.conf` и ссылка в
`multi-user.target.wants` без запущенного systemd, `Init` (каталоги §5 с режимами), интерфейс `Systemctl` с
фейком; раскладка `files` с sha256 и владением (§4.2, §6.5 remove).
**Файлы.** Создать: `internal/systemd/{cream-configure@.service,unit.go,stage.go,init.go,systemctl.go,fake.go,
*_test.go}`, `internal/systemd/testdata/golden/*.conf`, `internal/place/{place.go,place_test.go}`.
**Тесты первыми.**
- `stage_test`: golden drop-in для `system` с `after/before/wants` манифеста; `Enable` под `t.TempDir()` создаёт
  ссылку `…/multi-user.target.wants/cream-configure@<ext>.service → /usr/lib/systemd/system/cream-configure@.service`;
  `Disable` снимает ссылку и drop-in; неизвестная стадия → ошибка (Р3).
- `init_test`: дерево каталогов и режимы из §5 (`lock` 0700, `incoming` 0700); повтор идемпотентен.
- `systemctl_test`: на фейковом `ExecFunc` — точные argv (`--no-block`, `is-system-running`), разбор
  `starting|initializing|running|degraded`.
- `place_test`: файл ставится с `mode`, sha256 в записи; `dst` чужого расширения → 67 `conflict.file_owner`;
  `Unplace` — неизменённый удаляется, изменённый переименован в `.cream-modified` + предупреждение. Р11: `dst` в
  `/etc/cream/<B>/command-labels.d/x`, которого нет в реестре B, ставится и записывается в `FileRec` A; родительский
  каталог создаётся при отсутствии и при `Unplace` не удаляется; повтор `Place` того же файла A — не конфликт;
  отсутствующий при `Unplace` файл пропускается.
**Готово.** `make test PKG="./internal/systemd/... ./internal/place/..."`, `make lint` зелёные.
**Зависит от:** T1, T3, T4 (`manifest.File`), T6 (`state.FileRec`). От T8 (та же волна) не зависит: `Systemctl` получает `ExecFunc`, адаптер к `os/exec` — в `wire.go` (T12).

### Волна 4 — каталог, граф, ядро (T10 ∥ T11 ∥ T12)

#### Task 10: Индекс и валидация каталога, поиск ссылки
**Цель.** Правила каталога §4.1 и предупреждения валидатора (§6.3 `cream set` в `lock: none`, §7.3 `Exec*` с
`cream run` под блокировкой, §8.4 `systemctl` без `--no-block` в `apply`); поиск `[source/]name[@version]` (§9.3).
**Файлы.** Создать: `internal/catalog/{ref.go,validate.go,lookup.go,report.go,*_test.go}`. Фикстуры
`testdata/catalog`, `testdata/catalog-next`, `testdata/invalid` — готовые из T4, их не правит (нужна правка — стоп и
доклад).
**Тесты первыми.**
- `validate_test`: `testdata/catalog` — ноль ошибок; каждый `testdata/invalid/*` — ровно ожидаемый `error.code`;
  предупреждения на трёх специально «грязных» фикстурах.
- `ref_test`: таблица `ParseRef`.
- `lookup_test`: два источника — первый побеждает, затенение → предупреждение `source.shadowed`; `@version` не
  совпала → 66 `version.mismatch`; источник есть, но не синхронизирован → 69 `source.not_synced`; нигде нет →
  66 `not_found.extension`; `Digest` совпадает с `fsx.TreeDigest`.
**Готово.** `make test PKG="./internal/catalog/..."`, `make lint` зелёные.
**Зависит от:** T3, T4 (в том числе фикстуры), T7 (`source.SourceDir`).

#### Task 11: Резолвер
**Цель.** §9.4 и §12.4: платформа (family, arch с канонизацией, os по `/etc/os-release` под `Root`, commands),
применимость по фазе (`targets`), барьер архитектуры для `exec` (68 `platform.arch_barrier`), граф на
«установленное ∪ запрошенное», топосортировка, `Layer` с `token`, `params_digest`, `pinned`.
**Файлы.** Создать: `internal/resolve/{host.go,graph.go,layer.go,*_test.go}`, `internal/resolve/testdata/os-release/*`.
**Тесты первыми.** Таблица графа `{installed, requested, wantOrder | wantCode}`: `depends` с диапазоном (нет →
67 `deps.missing` со списком кандидатов; версия вне диапазона → 67); `requires` без поставщика → 67;
`conflicts.extensions` в обе стороны; `conflicts.capabilities` («другой поставщик»; сам себе не конфликт); владение
`files` → 67; цикл → 67 `deps.cycle`; порядок «поставщик раньше потребителя», детерминированный при равенстве (по
имени). Платформа: `family`, arch `aarch64`, os `ubuntu 22.04` против `>=22.04`, `commands` через фейковый
`HasCommand`; `live: no` → 68 `target.live`, `confirm` без `AcceptRisk` → 68 `target.confirm`; `build: none` в
фазе build → слой `mode: none`. `Dependents`. Golden JSON `Layer` против примера §12.4; `token` с 12 hex.
**Готово.** `make test PKG="./internal/resolve/..."`, `make lint` зелёные.
**Зависит от:** T2, T4, T6.

#### Task 12: Ядро исполнителя: настройки, apply, configure, `system init`
**Цель.** `Engine` и `RunHook` (окружение §6.3, фильтр секретов, коды 10/20 → реестр/`/run/reboot-required`,
79 с `hook_exit`), `Installed()`; `cream list/show/get/set/config get|export` (§8.3), apply по §8.4 (хук `apply`
→ иначе `--no-block restart cream-configure@` с маркером `RunReason` → иначе ничего; `--wait`; в стадии загрузки
`--wait` → `--no-block` с предупреждением; сбой apply → 79 после записи), `cream configure <ext> [--wait]` и
`--from-unit`, `cream system init`.
**Файлы.** Создать: `internal/engine/{engine.go,hook.go,installed.go,set.go,configure.go,system.go,*_test.go}`,
`internal/engine/helpers_test.go` (`newTestEngine`, `seedInstalled`, `fixtureDir`),
`internal/cli/helpers_test.go` (`runCLI`, `fixtureDir`), `internal/cli/wire.go` (сборка `Engine` из глобальных
флагов). Оба `helpers_test.go` и `engine.go` после волны 4 **заморожены**: T13–T15 их не правят. Заменить: `internal/cli/cmd_config.go`,
`internal/cli/cmd_system.go`. Golden: `internal/cli/testdata/golden/{list,show,get,set,config-export,configure,
system-init}*.json`.
**Тесты первыми.** Движок на `t.TempDir()`-корне, фейковых `Systemctl` и реальном `runner.Exec` с фикстурами
`with-config`, `apply-hook`, `configure-system` (реестр подготавливается вручную через `state.Store`):
- `set` держит `lock/<ext>` exclusive (параллельный `set` ждёт); изменённые ключи — в `CREAM_CHANGED_KEYS`;
  без `apply`-хука и с `configure` при systemd «running» — `Restart(noBlock=true)` и файл маркера; `--wait` →
  `noBlock=false`; состояние `starting` + `--wait` → `noBlock=true` и предупреждение; без systemd — ничего;
  `--no-apply` — ничего; хук `apply` вернул 1 → 79, а запись сохранена.
- `configure --from-unit` читает и удаляет маркер (`CREAM_REASON=apply`), без маркера — `boot`.
- `RunHook`: секрет из `secrets` в выводе хука заменён на `***`; код 10 → `reboot_required`; 20 без
  `--allow-skip` → 68.
- CLI: golden-формы, коды 64/65/66/77 по таблице (`show` чужого расширения → 66, `get --secret` без root при
  `--root /` → 77).
**Готово.** `make test PKG="./internal/engine/... ./internal/cli/..."`, `make lint` зелёные.
**Зависит от:** T4 (типы и фикстуры `testdata/catalog`), T5, T6, T8, T9.

### Волна 5 — жизненный цикл и действия (T13 ∥ T14 ∥ T15)

#### Task 13: `install` и `upgrade` на живой машине
**Цель.** §6.5: проверки §9.4 через `resolve`, копия в `.<ext>.new` + `rename`, `place`, запись параметров
(`SetOpts{AllowInstallScope, Only}`), хук, `systemd.Enable`, реестр, `history`; повтор по реестру (`installed` без
изменения `scope: install` → `changed:false` и `set --apply` прочих; изменение `scope: install` → хук; `failed` →
хук; `failed-upgrade` → повтор upgrade; `--reinstall`); на живой машине — `daemon-reload` при `files` и синхронный
`Configure(reason=install|upgrade)`, при `starting|initializing` — `--no-block restart` с предупреждением; `upgrade`
с `.<ext>.prev`, без хука `upgrade` — `install` с `CREAM_PREV_VERSION`; понижение → 65 `version.downgrade` (флаг `--allow-downgrade` — Р1);
та же версия с другим отпечатком → 65 `version.same_digest`; `--accept-risk`, `--allow-skip`, `--source`; несколько
ссылок — только без параметров (64).
**Файлы.** Создать: `internal/engine/{install.go,upgrade.go,tree.go,install_test.go,upgrade_test.go}`. Заменить:
`internal/cli/cmd_install.go`. Golden: `install-*.json`, `upgrade-*.json`.
**Тесты первыми.** На фикстурах `testdata/catalog` (синхронизированных в тесте через `source.Syncer` с `path`):
таблица сценариев — первая установка `noop` (`changed:true`, реестр по §10); двойная → `changed:false`, хук не
вызван (счётчик в `CREAM_STATE_DIR`); `failing-install` → `status: failed`, повтор вызывает хук; `install-scope`
с новым значением → хук вызван, со старым — нет; `depends-lib` без `lib-fixture` → 67, после — успех и порядок;
`conflict-*`, `requires-cap`, `live-confirm` без/с `--accept-risk`, `not-applicable` без/с `--allow-skip`
(`status: skipped`), `reboot-required` → поле и файл; `files-mode` — режим и sha256; файл A в `/etc/cream/<B>/…` (Р11) — в реестре A, повторный install A не
конфликтует сам с собой; `upgrade-fixture` 1.0.0 →
2.0.0 из `catalog-next` (успех удаляет `.prev`; сбой → `failed-upgrade`, `.prev` на месте); `--json` = запись
реестра + `changed`. Секреты из `KEY=-` не попадают ни в реестр, ни в `history`, ни в stdout.
**Готово.** `make test PKG="./internal/engine/... ./internal/cli/..."`, `make lint` зелёные.
**Зависит от:** T7, T9, T10, T11, T12.

#### Task 14: `remove`
**Цель.** §6.5 remove: отказ при зависимых (67 `deps.dependents`) и `--force`; хук `remove`, `systemd.Disable`,
`place.Unplace`, удаление дерева и реестра, `history`; `--purge` (`/etc/cream/<ext>`, `/var/lib/cream/ext/<ext>`);
`--forget` без хука; без хука `remove` и без `--forget` → 65 `hook.missing_remove` с подсказкой.
**Файлы.** Создать: `internal/engine/{remove.go,remove_test.go}`. Заменить: `internal/cli/cmd_remove.go`.
Golden: `remove-*.json`.
**Тесты первыми.** Реестр и дерево готовятся напрямую (`state.Store`, `fsx.CopyTree`) — без зависимости от T13:
таблица — зависимый есть / `--force`; хук упал → 79, реестр не тронут; изменённый `files` → `.cream-modified` и
предупреждение в `warnings`; `--purge` удаляет домен; `--forget` не вызывает хук; удаление библиотеки при
потребителе → 67.
**Готово.** `make test PKG="./internal/engine/... ./internal/cli/..."`, `make lint` зелёные.
**Зависит от:** T9, T11, T12.

#### Task 15: Действия: `cream run`, `cream actions`
**Цель.** §7.1–§7.2: флаги cream только до `<ext>`; `args: none|pairs|free`; `pairs` проверяются по `params` и
`config.keys`; секретный параметр — только `KEY=-`, значение со stdin cream → stdin действия, пустой → 64, в argv
→ 64; `stdin: pass`; `lock` exclusive/shared/none (+ `CREAM_LOCK_FD`); `timeout`; `phases` (Р5); `root`; `output:
json` — ровно один объект, иначе 76; код > 63 → 76; конверт `--json` `{"action","exit_code","output"}`; без
`--json` stdout насквозь, stderr через фильтр.
**Файлы.** Создать: `internal/engine/{action.go,action_args.go,action_test.go,action_args_test.go}`. Заменить:
`internal/cli/cmd_action.go`. Golden: `run-*.json`, `actions-*.json`.
**Тесты первыми.** Фикстуры `action-json`, `action-bad-protocol`, `lock-restart` (реестр — напрямую): таблица
argv-разбора; секрет доходит до stdin действия и отсутствует в `/proc/<pid>/cmdline` действия (действие само
печатает свой cmdline в stderr → проверка); два JSON-объекта → 76; код 64 → 76; `lock: none` не ждёт занятую
`lock/<ext>`; `exclusive` ждёт и падает 75 по `--lock-timeout`; действие с `phases: [live]` при `CREAM_PHASE=build`
→ 68; `cream actions --json` — golden. Golden-файлы `run-*.json` и `actions-*.json` (форма `result`) —
**контракт для stvor-agent** (спека stvor-agent §5.7, §12): после слияния T15 меняются только добавлением полей
(§11.4), и `docs/cli.md` (T21) описывает их как контракт.
**Готово.** `make test PKG="./internal/engine/... ./internal/cli/..."`, `make lint` зелёные.
**Зависит от:** T5, T6, T8, T12.

### Волна 6 — build, CLI каталога, упаковка (T16 ∥ T17 ∥ T18)

#### Task 16: Фаза build
**Цель.** §12.2 п.4–5: `cream install --phase build --from-dir D --record F --params-file P --remove-params-file`:
сверка манифеста и отпечатка с `record.json` (Р6; расхождение → 65 `record.mismatch`), `platform` и граф против
реестра гостя, `policy-rc.d` (exit 101) на время хука, если его не было, — и снятие после, в том числе при сбое;
configure только включается ссылками (без вызова); `params.env` 0600 root (77 иначе), удаление по флагу;
удаление `incoming/<id>` после успеха.
**Файлы.** Создать: `internal/engine/{build.go,policyrcd.go,build_test.go}`. Изменить: `internal/cli/cmd_install.go`
(флаги фазы build). Golden: `install-build-*.json`.
**Тесты первыми.** Корень `t.TempDir()`: успех — реестр `phase: build`, ссылка в `*.wants`, `policy-rc.d` снят,
`params.env` удалён, `incoming` удалён; чужой `policy-rc.d` сохранён нетронутым; хук упал → `policy-rc.d` снят,
`status: failed`; подмена файла в дереве → 65; `params_digest` не совпал → 65; секрет из `params.env` не в выводе.
**Готово.** `make test PKG="./internal/engine/... ./internal/cli/..."`, `make lint` зелёные.
**Зависит от:** T10 (`LoadExtension`), T11, T13.

#### Task 17: CLI источников и каталога
**Цель.** `cream source add|remove|list|show|sync` (`--archive --sha256`, `--path`, `--git` → 64 `usage.reserved`,
`add --sync` с откатом записи при сбое, `--replace`), `cream validate [--catalog DIR] [DIR…]`,
`cream catalog pack DIR --out F` (валидирует, затем `source.Pack`, `SOURCE_DATE_EPOCH`), `cream resolve [--phase]
[--target-arch] [--host-arch] [--family] <ref[ K=V…]>… --json` (офлайн, только кеш; секретные параметры помечены,
не печатаются).
**Файлы.** Заменить: `internal/cli/cmd_source.go`, `internal/cli/cmd_catalog.go`. Создать: их `_test.go`, golden
`source-*.json`, `validate-*.json`, `resolve-*.json`.
**Тесты первыми.** Сквозь `cli.Run` с `--root t.TempDir()`: `source add --path testdata/catalog --sync` →
`sources.lock`; повтор `add` → 0 без изменений; другие поля → 65, с `--replace` → 0; `add --archive` с голым путём
→ 64; `add --archive file://… --sha256 <неверный> --sync` → 69 и записи нет; `validate testdata/invalid/*` — коды;
`catalog pack` дважды — одинаковый sha256; `resolve --phase build --target-arch arm64 --host-arch amd64` слоя
`exec` → 68 `platform.arch_barrier`; golden `resolve --json` для `depends-lib`.
**Готово.** `make test PKG="./internal/cli/..."`, `make lint` зелёные.
**Зависит от:** T7, T10, T11.

#### Task 18: Упаковка `.deb`, тарболы, воспроизводимость, `govulncheck`
**Цель.** §15: nfpm в контейнере (образ `goreleaser/nfpm` с закреплённым тегом — проверить `docker pull` до
коммита и записать тег в Makefile), состав пакета, сценарии сопровождающего, тарболы, `SHA256SUMS`, проверка
воспроизводимости, `govulncheck`.
**Файлы.** Изменить: `Makefile` — цели `deb` (`ARCH=amd64|arm64`, `VERSION` без `v`), `dist-release` (оба `.deb`,
оба `cream_<v>_linux_<arch>.tar.gz` с `cream`, `LICENSE` (Apache-2.0, уже в корне репо), `NOTICE` и
шаблоном юнита, `SHA256SUMS`), `repro-check` (две сборки `dist-release` с одним
`SOURCE_DATE_EPOCH` = время коммита, `cmp` sha256), `vuln` (`go run golang.org/x/vuln/cmd/govulncheck@<пин>` в
контейнере — без записи в `go.mod`: `GOFLAGS=-mod=mod` в отдельном `GOMODCACHE`), `build`/`build-all` получают
`SOURCE_DATE_EPOCH`. Создать: `packaging/nfpm.yaml`, `packaging/scripts/{postinst,prerm,postrm}`,
`packaging/copyright`, `scripts/dist-release.sh`, `scripts/repro-check.sh`.
**Тесты первыми.** `scripts/repro-check.sh` пишется первым и падает (нет `deb`). Затем: `dpkg-deb -c` в контейнере
`debian:13` — точный список путей §15 (`/usr/bin/cream`, `/usr/lib/systemd/system/cream-configure@.service` —
**тот же файл**, что `internal/systemd/cream-configure@.service`, `/usr/lib/cream/guest/linux-{amd64,arm64}/cream`,
`/usr/share/doc/cream/{copyright,changelog.gz}`); `dpkg-deb -f` — `Architecture`, нет `Depends`, `Suggests: git,
libguestfs-tools`; shellcheck сценариев (lefthook). `postrm purge` удаляет `/etc/cream` и `/var/lib/cream` только
при пустом `installed/` — проверяется в T19 на живом контейнере.
**Готово.** `make deb ARCH=amd64`, `make deb ARCH=arm64`, `make repro-check` (sha256 совпали), `make vuln` — без
находок; `make lint` зелёный. Тяжёлое — под `flock $(HEAVY_LOCK)`.
**Зависит от:** T1, T9 (файл юнита).

### Волна 7 — живые проверки, CI, документация (T19 ∥ T20 ∥ T21)

#### Task 19: Интеграция в контейнерах с systemd + пилотные фикстуры
**Цель.** Сценарий §14 на `debian:12` и `ubuntu:24.04` (amd64; `debian:13` — локально по желанию, arm64 — в CI),
`make test-integration`; пилотный каталог внутри cream для сверки с потребителем (§17 «Сверка»).
**Файлы.** Изменить: `Makefile` — `test-integration` (`DISTRO ?= debian-12`, `DISTRO=all` = по очереди
`debian-12 ubuntu-24.04`; зависит от `deb`; весь прогон под `flock $(HEAVY_LOCK)`; лог —
`.scratchpad/test-runs/integration-<distro>-<время>/`). Создать: `test/integration/Dockerfile` (`ARG BASE`,
`systemd systemd-sysv ca-certificates procps`, `STOPSIGNAL SIGRTMIN+3`, `CMD ["/sbin/init"]`),
`test/integration/run.sh` (хост: сборка образа `cream-it:<distro>`, `docker run -d --privileged --cgroupns=host
-v /sys/fs/cgroup:/sys/fs/cgroup:rw --tmpfs /run --tmpfs /run/lock`, `docker cp` пакета и архива фикстур,
`docker restart` для проверки загрузки, `trap` — удаление своего контейнера), `test/integration/scenario/{lib.sh,
10-install.sh,20-config.sh,30-actions.sh,40-lock-restart.sh,50-upgrade-remove.sh,60-after-boot.sh,
55-cross-files.sh,70-secrets-audit.sh,80-purge.sh}`, `test/integration/capture-envfile.sh` и эталоны
`internal/envfile/testdata/systemd-{249,252,255,257}/*.out` (единственный владелец этих каталогов — T19); `testdata/pilot/cream-catalog.yaml` и `testdata/pilot/extensions/`:
`cloud-user` (`kind: library`, `get_cloud_user.sh` по мотивам `cream-extensions/lib/get_cloud_user.sh` — порт,
не копия), `fixture-docker-like` (`depends: [{name: cloud-user}]`, `configure` стадии `system` с `after:
[fixture-dockerd.service]`, юнит-пустышка через `files`, `live: confirm`), `fixture-teleport-like` (действия `join`
со `stdin: secret`, `set-managed` exclusive с синхронным `systemctl restart fixture-teleport.service`,
`managed-status`/`render`/`status` с `lock: none`, `render` в `ExecStartPre` юнита из `files`, ключ `scope:
install`, readonly-ключ, который пишет `set-managed` через `cream set --allow-readonly`), `fixture-agent-like`
(`depends: [{name: fixture-teleport-like}]`, `files` кладёт `/etc/cream/fixture-teleport-like/command-labels.d/agent`
— модель drop-in командной метки stvor-agent в каталог teleport).
**Тесты первыми.** Эталоны EnvironmentFile — первым шагом: `capture-envfile.sh` поднимает тот же
`test/integration/Dockerfile` с `BASE=ubuntu:22.04`(249), `debian:12`(252), `ubuntu:24.04`(255), `debian:13`(257)
по одному под `HEAVY_LOCK` и снимает `systemd-run --wait --pipe -p EnvironmentFile=… /usr/bin/env -0` по каждому
`cases/*.env`; после этого `reference_test` из T3 обязателен (`t.Skip` удаляется — `reference_test.go` правит T19,
T3 закрыта), расхождение 249/257 на `\` закреплено отдельной строкой. Сценарии пишутся до запуска и падают на
отсутствующем пакете. Сценарий `55-cross-files.sh` (Р11): install `fixture-agent-like` → файл на месте и в реестре
A, не в реестре B; повторный install → `changed:false`, файл цел; `remove fixture-agent-like` убирает только свой
файл, остальное содержимое `/etc/cream/fixture-teleport-like/` и реестр B не тронуты. Шаги §14: `dpkg -i`; `cream
catalog pack testdata/pilot` → `source add --archive file://… --sha256 … --sync`; install цепочки `cloud-user →
fixture-docker-like (--accept-risk) → fixture-teleport-like`; `set`/`apply`; `run join` с секретом со stdin;
двойной install → `changed:false`; install после `failed` → хук повторён; новое значение `scope: install` → хук,
`cream set` того же ключа → 65; `lock-restart`: `set-managed` укладывается во время restart, вариант
`lock-restart-exclusive` падает по `--lock-timeout` (75); upgrade и remove; `docker restart` → `cream-configure@*`
отработал на загрузке (`systemctl show -p ActiveState`, `history.jsonl`); аудит секретов: во время `join` (действие
спит 2 с) `ps -eo args` не содержит секрета; `journalctl`, `installed/*.json`, `history.jsonl` — тоже;
`apt purge cream` при пустом реестре удаляет `/etc/cream`, `/var/lib/cream`, `/var/cache/cream`.
**Готово.** `make test PKG="./internal/envfile/..."` зелёный без пропусков; `make test-integration DISTRO=debian-12` и `make test-integration DISTRO=ubuntu-24.04` — по очереди,
каждый exit 0, контейнеры удалены (`docker ps -a --filter name=cream-it` пуст).
**Зависит от:** T13–T18.

#### Task 20: CI-матрица и сборка ассетов релиза
**Цель.** На PR: lint/test/build (как сейчас) + `repro-check` + `vuln` + интеграция `debian-12` и `ubuntu-24.04`
на `ubuntu-latest`, `debian-13` на `ubuntu-24.04-arm` (§17 «Матрица MVP»). На релизе: job после release-please по
`release_created` — checkout тега, `make dist-release VERSION=<тег>`, `gh release upload` (`.deb` ×2, `.tar.gz`
×2, `SHA256SUMS`).
**Файлы.** Изменить: `.github/workflows/ci.yml` (jobs `check`, `integration` (matrix), `release-please` с
`outputs`, `release-assets` с `permissions: contents: write`), `.claude/skills/release/SKILL.md` (п.5 — что
прикладывается). Не трогать `Makefile` (цели — из T18/T19 по именам этого плана).
**Тесты первыми.** `actionlint` в контейнере (`rhysd/actionlint` с закреплённым тегом) — сначала на текущем
`ci.yml`, затем на новом; действия закреплены по SHA с комментарием версии, как сейчас. Сухой прогон
`release-assets` — `workflow_dispatch` с входом `tag` на ветке (после слияния плана; до этого — только actionlint).
**Готово.** `actionlint` чистый; на PR ветки `feature/cream-mvp` все jobs зелёные (проверяет контроллер в T23:
`gh pr checks`).
**Зависит от:** T18, T19 (имена целей; содержимое — нет).

#### Task 21: Документация
**Цель.** `docs/cli.md` — команды, флаги, коды §11.3, формы `result` по golden-файлам (ссылки на них); формы `run --json` и `actions --json` — отдельным
разделом «Контракт для stvor-agent» (правила изменения по §11.4); ADR на
трудно откатываемое; README и карта репозитория.
**Файлы.** Создать: `docs/cli.md`, `docs/adr/0001-contract-cream-v1.md` (формат, пути §5, коды выхода, Р1–Р11 как
следствия), `docs/adr/0002-single-dependency-stdlib-cli.md` (yaml — единственная зависимость, stdlib `flag`, nfpm
без goreleaser). Изменить: `docs/adr/README.md`, `docs/README.md` (строки: план, `cli.md`, ADR), `README.md`
(установка `.deb`, быстрый старт `source add --archive … --sync` → `install`), `AGENTS.md` (карта: `internal/*`,
`packaging/`, `test/`, `testdata/`; убрать «Сейчас — каркас»).
**Тесты первыми.** Проверка ссылок: каждый golden-файл упомянут в `cli.md` (`ls internal/cli/testdata/golden` против
grep — однострочник в отчёте, не скрипт в репо).
**Готово.** `make lint` зелёный; все относительные ссылки открываются; строки в обоих индексах.
**Зависит от:** T12–T17.

### Волна 8 — build на живом appliance

#### Task 22: `make test-build`
**Цель.** §14 «Build-фаза»: фикстурный qcow2 Debian 13 genericcloud (URL и sha256 закреплены в Makefile, кеш
`.cache/images/<sha256>.qcow2`), overlay `qemu-img create -b`, шаги §12.2 так, как их будет делать bisquite:
`--copy-in` гостевого cream, `system init`, `--tar-in` архива с `--owner=0 --group=0 --numeric-owner`, `install
--phase build --from-dir … --record … --params-file … --remove-params-file` для `fixture-docker-like` и
`fixture-teleport-like` из `testdata/pilot` (`record.json` — из `cream resolve --json` на хосте).
**Файлы.** Изменить: `Makefile` (`test-build`, ручной, под `flock $(HEAVY_LOCK)`). Создать: `test/build/run.sh`,
`test/build/Dockerfile` (`debian:13` + `libguestfs-tools qemu-utils linux-image-amd64`).
**Где запускать.** Основной путь — контейнер `test/build/Dockerfile` с `--device /dev/kvm` и
`LIBGUESTFS_BACKEND=direct` (Docker-first). Запасной — `virt-customize` хоста (есть: `/usr/bin/virt-customize`,
пользователь в группе `kvm`), включается `TEST_BUILD_HOST=1`. Выбор фиксируется в отчёте задачи.
**Тесты первыми.** `run.sh` с проверками пишется до прогона: после сборки `virt-cat` реестра (`phase: build`,
`status: installed`), `virt-ls` — ссылка в `multi-user.target.wants`, `policy-rc.d` отсутствует, `incoming/`
пуст, `params.env` удалён, владелец файлов дерева — 0:0; во время `virt-customize` фоновый `ps -eo args` на
хосте/в контейнере не содержит значений параметров; намеренно упавший слой (`failing-install` из
`testdata/catalog`) — текст ошибки без параметров.
**Готово.** `make test-build` exit 0 один раз; лог в `.scratchpad/test-runs/build-<время>.log`.
**Зависит от:** T16, T17, T18, T19 (пилотные фикстуры).

### Волна 9

#### Task 23: Финальная интеграция + `make ci`
**Цель.** Всё слито в `feature/cream-mvp`, сквозные проверки зелёные, PR в `main`, который выпускает `v1.0.0`.
**Файлы.** Изменить: `docs/STATUS.md`, этот план (финальные отметки), любые правки по итогам ревью.
**Шаги.** reviewer по всей ветке → fix-раунд → `make ci` (один прогон) → `make test-integration DISTRO=all` →
`make repro-check` → `make vuln` → проверка, что `go.mod` содержит ровно одну внешнюю зависимость → PR
`feat: cream 1.0 — формат cream/v1 и исполнитель` с футером `Release-As: 1.0.0` → `gh pr checks` зелёные (в том
числе arm64) → слияние squash → после релиза: ассеты на месте (`gh release view v1.0.0`), sha256 совпадают с
`SHA256SUMS`.
**Готово.** Вывод `make ci` exit 0 и зелёный `gh pr checks`; `docs/STATUS.md` — фаза «MVP выпущен», дальше — шаг 2
§16 (пилот в `cream-extensions`).
**Зависит от:** T1–T22.

## Риски исполнения
1. **Дрейф контрактов между волнами.** Пакеты пишутся параллельно по сигнатурам из этого плана; расхождение
   всплывёт только при слиянии. Меры: контроллер после каждой волны собирает `feature/cream-mvp` и гоняет `make ci`
   до старта следующей; правка экспортируемого API — только через контроллера и строкой в плане.
2. **Эталоны EnvironmentFile и systemd в Docker.** Захват 249/252/255/257 и интеграция — обе в T19 — требуют
   systemd PID 1 в контейнере на cgroup v2; до T19 сверка с systemd пропускается (`t.Skip` в T3), и расхождение
   разборщика с systemd всплывёт только в волне 7 (правка `internal/envfile` тогда — fix-задача контроллера).
   На раннерах GitHub (особенно arm64) поведение `--cgroupns=host` может отличаться. Меры: один
   `test/integration/Dockerfile` с `ARG BASE` для эталонов и сценария; захват эталонов — первый шаг T19; ранний
   пробный прогон на CI-ветке до финала;
   если arm-раннер не тянет systemd — job помечается и решение выносится владельцу, MVP не блокируется молча.
3. **Тест взаимной блокировки (§14 п.7) хрупок по времени.** Порог «за время restart, а не по таймауту» зависит от
   загрузки хоста. Меры: `--lock-timeout` негативного варианта 10 с, порог позитивного — половина этого; прогон
   только под `HEAVY_LOCK`.
4. **Нагрузка на хост (12 CPU, свободно ~7 ГБ RAM, другие стенды).** `-race` по всему модулю и privileged-контейнеры параллельно
   с исполнителями. Меры: `TEST_P=4`, исполнители гоняют `make test PKG="<свои пакеты>"`, всё тяжёлое — через
   `flock $(HEAVY_LOCK)` и только у контроллера/T19/T22; ≤ 3 исполнителя.
5. **libguestfs в контейнере** (supermin, ядро, `/dev/kvm`) может не завестись. Мера: запасной путь через
   `virt-customize` хоста (T22), задача не блокирует T23, если хотя бы один путь дал exit 0.

## Вне MVP
Всё отложенное из §17 спеки (резервируется по Р1, возвращается минорными версиями с подъёмом `cream:`):
git-источник (`type: git`, `pin`, `--require-pinned`); `targets.build: files` и `cream stage`; `bin`; `cream
assume` и `assumed.json`; `install --with-deps`; `cream verify`; команда `cream lineage`; `cream search`/`info`;
хуки `secret`, `check` и команды `check`/`status`; стадии `early`, `cloud-user`, `graphical`; `--allow-downgrade`.
Также вне плана: полная матрица §14 (`debian:13`/`ubuntu:22.04` amd64, `ubuntu:24.04` arm64), Jetson/L4T,
dispatch `cream-bump.yml` в `cream-extensions` и `bisquite` (§3 — после появления этих workflow), ночной прогон
`make test-build`, шаги 2–4 §16 (пилот каталога, весь каталог, bisquite) — каждый своим планом.
