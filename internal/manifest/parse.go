package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/semver"
)

// maxManifestSize — предел размера extension.yaml и cream-catalog.yaml.
const maxManifestSize = 1 << 20

// Коды ошибок пакета.
const (
	codeSchema       = "manifest.schema"
	codeInvalid      = "manifest.invalid"
	codeReserved     = "manifest.reserved"
	codeCreamVersion = "platform.cream_version"
	codeDevBuild     = "platform.dev_build"
	codeCatSchema    = "catalog.schema"
	codeCatInvalid   = "catalog.invalid"
)

// reservedText — текст отказа для отложенного из §17 (рулинг Р1).
const reservedText = "зарезервировано в cream/v1, не реализовано в cream 1.0"

// Сырые формы для декодирования. Строковые поля принимают любой скаляр как
// записан (yaml.v3 отдаёт исходный текст), поэтому `default: 0` и `mode: 0644`
// приходят строками "0" и "0644"; булевы поля — только true/false.
type rawManifest struct {
	Schema    string               `yaml:"schema"`
	Name      string               `yaml:"name"`
	Version   string               `yaml:"version"`
	Summary   string               `yaml:"summary"`
	Cream     string               `yaml:"cream"`
	Kind      string               `yaml:"kind"`
	Platform  *rawPlatform         `yaml:"platform"`
	Targets   rawTargets           `yaml:"targets"`
	Provides  []string             `yaml:"provides"`
	Requires  []string             `yaml:"requires"`
	Depends   []rawDep             `yaml:"depends"`
	Conflicts rawConflicts         `yaml:"conflicts"`
	Hooks     map[string]rawHook   `yaml:"hooks"`
	Config    rawConfig            `yaml:"config"`
	Actions   map[string]rawAction `yaml:"actions"`
	Files     []rawFile            `yaml:"files"`
}

type rawPlatform struct {
	Family   string     `yaml:"family"`
	Arch     []string   `yaml:"arch"`
	OS       []rawOSReq `yaml:"os"`
	Commands []string   `yaml:"commands"`
}

type rawOSReq struct {
	ID      string  `yaml:"id"`
	Version *string `yaml:"version"`
}

type rawTargets struct {
	Build    string `yaml:"build"`
	Live     string `yaml:"live"`
	LiveNote string `yaml:"live_note"`
}

type rawDep struct {
	Name    string  `yaml:"name"`
	Version *string `yaml:"version"`
}

type rawConflicts struct {
	Extensions   []string `yaml:"extensions"`
	Capabilities []string `yaml:"capabilities"`
}

type rawHook struct {
	Timeout string   `yaml:"timeout"`
	Stage   string   `yaml:"stage"`
	After   []string `yaml:"after"`
	Before  []string `yaml:"before"`
	Wants   []string `yaml:"wants"`
}

type rawConfig struct {
	Keys map[string]rawKey `yaml:"keys"`
}

type rawKey struct {
	Type        string  `yaml:"type"`
	Default     *string `yaml:"default"`
	Empty       bool    `yaml:"empty"`
	Secret      string  `yaml:"secret"`
	Readonly    bool    `yaml:"readonly"`
	Description string  `yaml:"description"`
	Pattern     bool    `yaml:"pattern"`
	Scope       string  `yaml:"scope"`
}

type rawAction struct {
	Summary string              `yaml:"summary"`
	Args    string              `yaml:"args"`
	Params  map[string]rawParam `yaml:"params"`
	Stdin   string              `yaml:"stdin"`
	Lock    string              `yaml:"lock"`
	Output  string              `yaml:"output"`
	Timeout string              `yaml:"timeout"`
	Phases  []string            `yaml:"phases"`
	Root    *bool               `yaml:"root"`
}

type rawParam struct {
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Secret   bool   `yaml:"secret"`
}

type rawFile struct {
	Src  string `yaml:"src"`
	Dst  string `yaml:"dst"`
	Mode string `yaml:"mode"`
}

// Parse разбирает и проверяет extension.yaml. cream — версия исполнителя:
// манифест, чей диапазон cream: её не содержит, отвергается кодом 68.
// Порядок проверок: schema → cream: → структура (неизвестные поля, типы) →
// правила §4.2, так что старый cream отказывает по cream:, а не по полю, которое
// появилось в новой минорной версии.
//
// w собирает предупреждения (platform.dev_build для сборки без версии); может быть nil.
func Parse(data []byte, cream CreamBuild, w *cerr.Warnings) (*Manifest, error) {
	root, err := parseNode(data, codeInvalid, "манифест")
	if err != nil {
		return nil, err
	}
	if s := topScalar(root, "schema"); s != SchemaV1 {
		if s == "" {
			return nil, cerr.New(cerr.ExitData, codeSchema, "манифест: нет поля schema (нужно %s; layout 1/2 не поддерживаются)", SchemaV1)
		}
		return nil, cerr.New(cerr.ExitData, codeSchema, "манифест: неизвестная схема %q (нужно %s)", s, SchemaV1)
	}
	creamRange, err := checkCream(topScalar(root, "cream"), cream, w)
	if err != nil {
		return nil, err
	}
	// Отложенные поля верхнего уровня (§17): отказ «зарезервировано», а не «неизвестное поле».
	if hasKey(root, "bin") {
		return nil, reserved("bin")
	}
	var raw rawManifest
	if err := decodeStrict(root, &raw, codeInvalid); err != nil {
		return nil, err
	}
	m, err := validate(&raw)
	if err != nil {
		return nil, err
	}
	m.Cream = creamRange
	return m, nil
}

// Load читает dir/extension.yaml и разбирает его Parse.
func Load(dir string, cream CreamBuild, w *cerr.Warnings) (*Manifest, error) {
	path := filepath.Join(dir, ManifestFile)
	data, err := readLimited(path, "not_found.manifest", codeInvalid)
	if err != nil {
		return nil, err
	}
	m, err := Parse(data, cream, w)
	if err != nil {
		return nil, wrapPath(err, path)
	}
	return m, nil
}

func readLimited(path, notFound, tooBig string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, cerr.Wrap(err, cerr.ExitNotFound, notFound, "нет файла %s", path)
		}
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "чтение %s", path)
	}
	defer f.Close() //nolint:errcheck // только чтение: ошибка Close ничего не теряет
	data, err := io.ReadAll(io.LimitReader(f, maxManifestSize+1))
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "чтение %s", path)
	}
	if len(data) > maxManifestSize {
		return nil, cerr.New(cerr.ExitData, tooBig, "%s: больше %d байт", path, maxManifestSize)
	}
	return data, nil
}

// wrapPath добавляет путь файла к сообщению, сохраняя код выхода и error.code.
func wrapPath(err error, path string) error {
	var e *cerr.Error
	if errors.As(err, &e) {
		out := *e
		out.Message = path + ": " + e.Message
		return &out
	}
	return err
}

// parseNode разбирает ровно один YAML-документ-отображение и вырезает ключи x-*.
func parseNode(data []byte, code, what string) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, cerr.New(cerr.ExitData, code, "%s: пустой документ", what)
		}
		return nil, cerr.Wrap(err, cerr.ExitData, code, "%s: разбор YAML", what)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, cerr.New(cerr.ExitData, code, "%s: больше одного YAML-документа", what)
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, cerr.New(cerr.ExitData, code, "%s: ожидалось отображение верхнего уровня", what)
	}
	stripExtensions(root, map[*yaml.Node]bool{})
	return root, nil
}

// stripExtensions удаляет пары с ключом "x-…" во всех отображениях дерева.
func stripExtensions(n *yaml.Node, seen map[*yaml.Node]bool) {
	if n == nil || seen[n] {
		return
	}
	seen[n] = true
	switch n.Kind {
	case yaml.MappingNode:
		kept := n.Content[:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind == yaml.ScalarNode && strings.HasPrefix(k.Value, "x-") {
				continue
			}
			kept = append(kept, k, n.Content[i+1])
		}
		n.Content = kept
	case yaml.AliasNode:
		stripExtensions(n.Alias, seen)
		return
	}
	for _, c := range n.Content {
		stripExtensions(c, seen)
	}
}

// hasKey — есть ли ключ верхнего уровня.
func hasKey(root *yaml.Node, key string) bool {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return true
		}
	}
	return false
}

// topScalar — значение скалярного ключа верхнего уровня или "".
func topScalar(root *yaml.Node, key string) string {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key && root.Content[i+1].Kind == yaml.ScalarNode {
			return root.Content[i+1].Value
		}
	}
	return ""
}

// CreamBuild — версия исполнителя для проверки cream: (рулинг Р18).
type CreamBuild struct {
	// Version — версия сборки; при Dev не используется.
	Version semver.Version
	// Dev — сборка без semver-версии ("dev", пусто, метаданные +…): она
	// удовлетворяет любому cream: с предупреждением platform.dev_build.
	Dev bool
}

// ParseCreamBuild разбирает версию сборки (version.Version): ведущее "v"
// отбрасывается; всё, что не строгий semver, — Dev.
func ParseCreamBuild(raw string) CreamBuild {
	v, err := semver.Parse(strings.TrimPrefix(strings.TrimSpace(raw), "v"))
	if err != nil {
		return CreamBuild{Dev: true}
	}
	return CreamBuild{Version: v}
}

// String — версия для сообщений.
func (b CreamBuild) String() string {
	if b.Dev {
		return "dev"
	}
	return b.Version.String()
}

// checkCream проверяет диапазон cream: — нижняя граница мажора равна мажору
// схемы (1), верхней нет или она тоже 1, и версия исполнителя в диапазоне.
// Предрелизная сборка сравнивается по основной версии (1.0.0-rc.1 — как
// 1.0.0); сборка Dev проходит с предупреждением (Р18).
func checkCream(s string, cream CreamBuild, w *cerr.Warnings) (semver.Range, error) {
	if s == "" {
		return semver.Range{}, cerr.New(cerr.ExitData, codeInvalid, "cream: обязательное поле (например \">=1.0.0\")")
	}
	r, err := semver.ParseRange(s)
	if err != nil {
		return semver.Range{}, cerr.Wrap(err, cerr.ExitData, codeInvalid, "cream")
	}
	lo, hi, ok := r.MajorBound()
	if !ok || lo != 1 || (hi != 1 && hi != ^uint64(0)) {
		return semver.Range{}, cerr.New(cerr.ExitData, codeInvalid,
			"cream: диапазон %q должен ограничивать мажор исполнителя мажором схемы cream/v1 (1), например \">=1.0.0\"", s)
	}
	if cream.Dev {
		if w != nil {
			w.Add(codeDevBuild, "сборка cream без версии (dev) считается удовлетворяющей cream: %s", r.String())
		}
		return r, nil
	}
	core := cream.Version
	core.Pre = nil
	if !r.Contains(core) {
		e := cerr.New(cerr.ExitNotApplicable, codeCreamVersion,
			"манифест требует cream %s, а это cream %s", r.String(), cream.String())
		e.Details = map[string]any{"required": r.String(), "cream": cream.String()}
		return semver.Range{}, e
	}
	return r, nil
}

// decodeStrict сверяет ключи отображений с полями out (неизвестное — ошибка
// с путём и строкой) и декодирует узел.
func decodeStrict(root *yaml.Node, out any, code string) error {
	if err := checkKnown(root, reflect.TypeOf(out).Elem(), "", map[*yaml.Node]bool{}); err != nil {
		return cerr.New(cerr.ExitData, code, "%s", err.Error())
	}
	if err := root.Decode(out); err != nil {
		return cerr.New(cerr.ExitData, code, "%s", strings.ReplaceAll(err.Error(), "line ", "строка "))
	}
	return nil
}

// checkKnown обходит узел вместе с типом Go. Несовпадение вида узла и типа не
// ошибка здесь: его сообщит Decode.
func checkKnown(n *yaml.Node, t reflect.Type, path string, seen map[*yaml.Node]bool) error {
	if n.Kind == yaml.AliasNode {
		if seen[n] {
			return nil
		}
		seen[n] = true
		return checkKnown(n.Alias, t, path, seen)
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeOf(yaml.Node{}) {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return nil
		}
		fields := map[string]reflect.Type{}
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			fields[name] = f.Type
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			ft, ok := fields[k.Value]
			if !ok {
				return fmt.Errorf("строка %d: неизвестное поле %s", k.Line, join(path, k.Value))
			}
			if err := checkKnown(n.Content[i+1], ft, join(path, k.Value), seen); err != nil {
				return err
			}
		}
	case reflect.Map:
		if n.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if err := checkKnown(n.Content[i+1], t.Elem(), join(path, n.Content[i].Value), seen); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return nil
		}
		for i, c := range n.Content {
			if err := checkKnown(c, t.Elem(), fmt.Sprintf("%s[%d]", path, i), seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
