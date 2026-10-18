// Package semver — версии SemVer 2.0 без метаданных сборки и диапазоны-конъюнкции
// компараторов (§4.2, §11.1 спеки): своя реализация вместо внешней зависимости.
package semver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Version — версия X.Y.Z[-pre]. Pre — идентификаторы pre-release через точку.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 []string
}

var errEmpty = errors.New("пустая версия")

// Parse разбирает строгую версию X.Y.Z[-pre]: без префикса v, без ведущих нулей
// и без метаданных сборки (+…).
func Parse(s string) (Version, error) {
	v, err := parse(s, false)
	if err != nil {
		return Version{}, fmt.Errorf("версия %q: %w", s, err)
	}
	return v, nil
}

// ParseLoose разбирает версию из одного–трёх числовых компонентов с
// необязательным pre-release: "12", "22.04", "24.04.1". Недостающие компоненты —
// нули, ведущие нули допустимы. Нужна для VERSION_ID из os-release и для
// границ диапазонов ("<5").
func ParseLoose(s string) (Version, error) {
	v, err := parse(s, true)
	if err != nil {
		return Version{}, fmt.Errorf("версия %q: %w", s, err)
	}
	return v, nil
}

func parse(s string, loose bool) (Version, error) {
	if s == "" {
		return Version{}, errEmpty
	}
	if strings.ContainsRune(s, '+') {
		return Version{}, errors.New("метаданные сборки (+…) не поддерживаются")
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if loose {
		if len(parts) > 3 {
			return Version{}, errors.New("больше трёх числовых компонентов")
		}
	} else if len(parts) != 3 {
		return Version{}, errors.New("ожидается X.Y.Z")
	}
	var nums [3]uint64
	for i, p := range parts {
		n, err := parseNum(p, !loose)
		if err != nil {
			return Version{}, err
		}
		nums[i] = n
	}
	v := Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}
	if hasPre {
		ids, err := parsePre(pre)
		if err != nil {
			return Version{}, err
		}
		v.Pre = ids
	}
	return v, nil
}

func parseNum(p string, strict bool) (uint64, error) {
	if p == "" {
		return 0, errors.New("пустой числовой компонент")
	}
	if !allDigits(p) {
		return 0, fmt.Errorf("компонент %q — не число", p)
	}
	if strict && len(p) > 1 && p[0] == '0' {
		return 0, fmt.Errorf("компонент %q с ведущим нулём", p)
	}
	n, err := strconv.ParseUint(p, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("компонент %q вне диапазона", p)
	}
	return n, nil
}

func parsePre(pre string) ([]string, error) {
	if pre == "" {
		return nil, errors.New("пустой pre-release")
	}
	ids := strings.Split(pre, ".")
	for _, id := range ids {
		if id == "" {
			return nil, errors.New("пустой идентификатор pre-release")
		}
		for _, c := range id {
			if !isAlnumHyphen(c) {
				return nil, fmt.Errorf("недопустимый знак %q в pre-release", c)
			}
		}
		if allDigits(id) && len(id) > 1 && id[0] == '0' {
			return nil, fmt.Errorf("числовой идентификатор pre-release %q с ведущим нулём", id)
		}
	}
	return ids, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// Compare возвращает -1, 0 или 1 по правилам приоритета SemVer 2.0 §11.
func Compare(a, b Version) int {
	if c := cmpUint(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmpUint(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := cmpUint(a.Patch, b.Patch); c != 0 {
		return c
	}
	switch {
	case len(a.Pre) == 0 && len(b.Pre) == 0:
		return 0
	case len(a.Pre) == 0:
		return 1
	case len(b.Pre) == 0:
		return -1
	}
	for i := 0; i < len(a.Pre) && i < len(b.Pre); i++ {
		if c := cmpIdent(a.Pre[i], b.Pre[i]); c != 0 {
			return c
		}
	}
	return cmpUint(uint64(len(a.Pre)), uint64(len(b.Pre)))
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cmpIdent: числовые меньше буквенно-цифровых, числовые — по значению
// (без ведущих нулей сравнение по длине, затем лексикографически), прочие — по ASCII.
func cmpIdent(a, b string) int {
	an, bn := allDigits(a), allDigits(b)
	switch {
	case an && bn:
		if c := cmpUint(uint64(len(a)), uint64(len(b))); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

// String — каноническая запись X.Y.Z[-pre].
func (v Version) String() string {
	s := strconv.FormatUint(v.Major, 10) + "." + strconv.FormatUint(v.Minor, 10) + "." + strconv.FormatUint(v.Patch, 10)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

// isAlnumHyphen: ASCII-буква, цифра или дефис.
func isAlnumHyphen(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-'
}
