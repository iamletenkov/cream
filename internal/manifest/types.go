// Package manifest — строгий разбор манифеста расширения cream/v1
// (extension.yaml, §4.2 и §7.1 спеки) и файла каталога cream-catalog.yaml (§4.1).
//
// Разбор строгий: неизвестное поле — ошибка, кроме ключей с префиксом "x-" на
// любом уровне отображений (они вырезаются до декодирования, поэтому имена
// действий и хуков вида "x-…" тоже невидимы); скаляр вместо списка — ошибка;
// отложенное (§17) — ошибка 65 manifest.reserved, а не «неизвестное поле»;
// поле минорной версии новее 1.0 (таблица sinceFields: стадии configure 1.1.0,
// every_boot и scope: build 1.2.0) при cream: ниже её —
// 65 manifest.invalid «нужен cream: ">=X"». Проверки дерева расширения (имя каталога, биты исполнения, ссылки,
// соответствие хуков файлам) — дело пакета catalog.
//
// Все ошибки — *cerr.Error: 65 manifest.* (данные), 68 platform.cream_version
// (манифест требует более новый cream), 66 not_found.* и 74 io.read у Load.
package manifest

import (
	"io/fs"
	"time"

	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/semver"
)

// SchemaV1 — единственная схема манифеста и каталога, которую знает cream 1.x.
const SchemaV1 = "cream/v1"

// ManifestFile и CatalogFile — имена файлов в каталоге расширения и в корне каталога.
const (
	ManifestFile = "extension.yaml"
	CatalogFile  = "cream-catalog.yaml"
)

// Kind — вид расширения.
type Kind string

// Виды расширения §4.2: library — только файлы, без хуков (§4.5).
const (
	KindExtension Kind = "extension"
	KindLibrary   Kind = "library"
)

// Семейства платформы (platform.family): deb — с cream 1.0, openwrt — с
// cream 1.3 (ADR-0005: OpenWrt 25.12+, procd, состояние в /opt/cream/var).
const (
	FamilyDeb     = "deb"
	FamilyOpenWrt = "openwrt"
)

// Platform — требования к машине (§4.2). Arch канонизирован (amd64, arm64);
// OS == nil — любая ОС.
type Platform struct {
	Family   string
	Arch     []string
	OS       []OSReq
	Commands []string
}

// OSReq — ID из /etc/os-release и необязательный диапазон VERSION_ID.
type OSReq struct {
	ID      string
	Version *semver.Range
}

// BuildMode — как ставить при сборке образа.
type BuildMode string

// Значения targets.build; "files" зарезервировано (§4.4).
const (
	BuildExec BuildMode = "exec"
	BuildNone BuildMode = "none"
)

// LiveMode — применимость к работающей машине.
type LiveMode string

// Значения targets.live.
const (
	LiveYes     LiveMode = "yes"
	LiveConfirm LiveMode = "confirm"
	LiveNo      LiveMode = "no"
)

// Targets — ось «нужен ли гость» (§4.4). LiveNote обязателен при confirm и no.
type Targets struct {
	Build    BuildMode
	Live     LiveMode
	LiveNote string
}

// Dep — зависимость по имени; Version == nil — любая версия.
type Dep struct {
	Name    string
	Version *semver.Range
}

// Conflicts — конфликты по именам и по способностям (§4.2).
type Conflicts struct {
	Extensions   []string
	Capabilities []string
}

// HookName — имя хука; файл — hooks/<имя>.
type HookName string

// Хуки cream 1.0 (§6.1). secret и check зарезервированы (рулинг Р4).
const (
	HookInstall   HookName = "install"
	HookUpgrade   HookName = "upgrade"
	HookConfigure HookName = "configure"
	HookApply     HookName = "apply"
	HookRemove    HookName = "remove"
)

// Таймауты хуков по умолчанию (§6.1). У install таймаута нет (0), у upgrade —
// как у install.
const (
	DefaultConfigureTimeout = 15 * time.Minute
	DefaultApplyTimeout     = 60 * time.Second
	DefaultRemoveTimeout    = 30 * time.Minute
	// MaxEarlyConfigureTimeout bounds configure of the early stage (cream
	// 1.1.1): the boot waits for it before sysinit.target. It is also the
	// default there; a larger timeout is clamped with manifest.early_timeout.
	MaxEarlyConfigureTimeout = 2 * time.Minute
)

// Stage — стадия загрузки хука configure (§6.4).
type Stage string

// Стадии §6.4. В cream 1.0 — только system; early, cloud-user и graphical
// появились в cream 1.1.0: манифест с ними обязан требовать cream: ">=1.1.0".
const (
	StageSystem    Stage = "system"
	StageEarly     Stage = "early"
	StageCloudUser Stage = "cloud-user"
	StageGraphical Stage = "graphical"
)

// Hook — объявление хука. Timeout == 0 — без таймаута; значения по умолчанию
// уже подставлены. Stage, After, Before, Wants, EveryBoot — только у configure.
// EveryBoot (cream 1.2.0, ADR-0004) — configure идёт на каждой загрузке, мимо
// отметки выполнения.
type Hook struct {
	Timeout              time.Duration
	Stage                Stage
	After, Before, Wants []string
	EveryBoot            bool
}

// ArgsMode — как разбираются аргументы действия (§7.1).
type ArgsMode string

// Значения args.
const (
	ArgsNone  ArgsMode = "none"
	ArgsPairs ArgsMode = "pairs"
	ArgsFree  ArgsMode = "free"
)

// StdinMode — что действие получает на stdin.
type StdinMode string

// Значения stdin.
const (
	StdinNone   StdinMode = "none"
	StdinSecret StdinMode = "secret"
	StdinPass   StdinMode = "pass"
)

// LockMode — блокировка домена на время действия (§6.3).
type LockMode string

// Значения lock.
const (
	LockExclusive LockMode = "exclusive"
	LockShared    LockMode = "shared"
	LockNone      LockMode = "none"
)

// OutputMode — протокол stdout действия (§7.2).
type OutputMode string

// Значения output.
const (
	OutputText OutputMode = "text"
	OutputJSON OutputMode = "json"
)

// Phase — фаза, в которой допустимо действие.
type Phase string

// Фазы cream.
const (
	PhaseBuild Phase = "build"
	PhaseLive  Phase = "live"
)

// DefaultActionTimeout — таймаут действия по умолчанию (§7.1).
const DefaultActionTimeout = 2 * time.Minute

// Action — объявление действия; файл — actions/<имя>. Значения по умолчанию
// (args none, stdin none, lock exclusive, output text, timeout 2m, phases [live],
// root true) уже подставлены.
type Action struct {
	Summary string
	Args    ArgsMode
	Params  map[string]Param
	Stdin   StdinMode
	Lock    LockMode
	Output  OutputMode
	Timeout time.Duration
	Phases  []Phase
	Root    bool
}

// Param — параметр действия с args: pairs. Type всегда задан: явный или из
// config.keys (точное имя или шаблон PREFIX_*). Secret — явный secret: true или
// secret: file у одноимённого ключа config.keys.
type Param struct {
	Type     *keytype.Type
	Required bool
	Secret   bool
}

// File — файл, который раскладывает cream (§4.2). Src — путь внутри дерева
// расширения; Dst — абсолютный чистый путь на машине. Mode == 0 — режим не
// задан: раскладка берёт 0755 или 0644 по биту исполнения Src.
type File struct {
	Src, Dst string
	Mode     fs.FileMode
}

// Manifest — разобранный и проверенный extension.yaml. Config — config.keys
// (шаблоны — под именем "PREFIX_*" с Pattern: true).
type Manifest struct {
	Schema, Name       string
	Version            semver.Version
	Summary            string
	Cream              semver.Range
	Kind               Kind
	Platform           Platform
	Targets            Targets
	Provides, Requires []string
	Depends            []Dep
	Conflicts          Conflicts
	Hooks              map[HookName]Hook
	Config             keytype.Schema
	Actions            map[string]Action
	Files              []File
}

// Catalog — разобранный cream-catalog.yaml. Roots — относительные чистые пути
// каталогов, где лежат расширения.
type Catalog struct {
	Schema, Name string
	Version      semver.Version
	Roots        []string
}
