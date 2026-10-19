package keytype

import (
	"errors"
	"strings"
)

// SecretMode — где хранится значение секретного ключа.
type SecretMode string

// Режимы секрета §8.2. SecretHook распознаётся, но манифест cream/v1 его отвергает (рулинг Р4).
const (
	SecretNone SecretMode = ""
	SecretFile SecretMode = "file"
	SecretHook SecretMode = "hook"
)

// Scope — область ключа: настройка или параметр установки.
type Scope string

// Области ключа §8.2.
const (
	ScopeConfig  Scope = "config"
	ScopeInstall Scope = "install"
)

// Spec — описание ключа config.keys.
type Spec struct {
	Type        Type
	Default     string
	Empty       bool
	Secret      SecretMode
	Readonly    bool
	Description string
	Pattern     bool
	Scope       Scope
}

// CheckValue проверяет значение ключа: пустое допустимо только при Empty,
// непустое — по типу.
func (s Spec) CheckValue(v string) error {
	if v == "" {
		if s.Empty {
			return nil
		}
		return errors.New("пустое значение не допускается (empty: false)")
	}
	return s.Type.Check(v)
}

// Schema — ключи домена по имени; шаблоны записаны как "PREFIX_*" с Pattern: true.
type Schema map[string]Spec

// Lookup ищет описание ключа: сначала точное имя, затем шаблон PREFIX_* с самым
// длинным префиксом. Хвост после префикса не может быть пустым.
func (s Schema) Lookup(key string) (Spec, bool) {
	if key == "" {
		return Spec{}, false
	}
	if sp, ok := s[key]; ok {
		return sp, true
	}
	var best Spec
	bestLen := -1
	for name, sp := range s {
		if !sp.Pattern {
			continue
		}
		prefix, ok := strings.CutSuffix(name, "*")
		if !ok || len(key) <= len(prefix) || !strings.HasPrefix(key, prefix) {
			continue
		}
		if len(prefix) > bestLen {
			best, bestLen = sp, len(prefix)
		}
	}
	return best, bestLen >= 0
}
