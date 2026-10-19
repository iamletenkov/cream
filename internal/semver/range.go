package semver

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

type op string

const (
	opGE op = ">="
	opLE op = "<="
	opGT op = ">"
	opLT op = "<"
	opEQ op = "="
)

type comparator struct {
	op  op
	ver Version
	raw string // версия как записана, для String()
}

// Range — конъюнкция компараторов через пробел: ">=4.0.0 <5". Дизъюнкция ("||"),
// "~", "^", дефисные диапазоны и голая версия без оператора не поддерживаются.
// Версии в компараторах разбираются ParseLoose ("<5", ">=22.04"). Верхняя граница
// "<X.Y.Z" без предрелиза исключает предрелизы X.Y.Z ("<5" ≡ "<5.0.0-0"),
// согласованно с MajorBound.
type Range struct {
	cmps []comparator
}

// ParseRange разбирает диапазон. Оператор может быть отделён от версии пробелом (">= 12").
func ParseRange(s string) (Range, error) {
	if strings.Contains(s, "||") {
		return Range{}, fmt.Errorf("диапазон %q: «||» не поддерживается, только конъюнкция через пробел", s)
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return Range{}, errors.New("пустой диапазон версий")
	}
	var r Range
	for i := 0; i < len(fields); i++ {
		tok := fields[i]
		o := leadingOp(tok)
		if o == "" {
			return Range{}, fmt.Errorf("диапазон %q: у %q нет оператора (>=, <=, >, <, =)", s, tok)
		}
		rest := tok[len(o):]
		if rest == "" {
			if i+1 >= len(fields) {
				return Range{}, fmt.Errorf("диапазон %q: после %q нет версии", s, o)
			}
			i++
			rest = fields[i]
		}
		v, err := ParseLoose(rest)
		if err != nil {
			return Range{}, fmt.Errorf("диапазон %q: %w", s, err)
		}
		r.cmps = append(r.cmps, comparator{op: o, ver: v, raw: rest})
	}
	return r, nil
}

func leadingOp(tok string) op {
	for _, o := range []op{opGE, opLE, opGT, opLT, opEQ} {
		if strings.HasPrefix(tok, string(o)) {
			return o
		}
	}
	return ""
}

// Contains — входит ли версия во все компараторы диапазона.
func (r Range) Contains(v Version) bool {
	for _, c := range r.cmps {
		d := Compare(v, c.ver)
		var ok bool
		switch c.op {
		case opGE:
			ok = d >= 0
		case opLE:
			ok = d <= 0
		case opGT:
			ok = d > 0
		case opLT:
			// "<X.Y.Z" без предрелиза исключает и предрелизы X.Y.Z: "<5" ≡ "<5.0.0-0".
			ok = d < 0 && (len(c.ver.Pre) > 0 || len(v.Pre) == 0 || !sameCore(v, c.ver))
		case opEQ:
			ok = d == 0
		}
		if !ok {
			return false
		}
	}
	return true
}

func sameCore(a, b Version) bool {
	return a.Major == b.Major && a.Minor == b.Minor && a.Patch == b.Patch
}

// String — компараторы через один пробел, версии — как записаны.
func (r Range) String() string {
	parts := make([]string, len(r.cmps))
	for i, c := range r.cmps {
		parts[i] = string(c.op) + c.raw
	}
	return strings.Join(parts, " ")
}

// MajorBound возвращает наименьший и наибольший мажор, который допускает диапазон.
// hi = math.MaxUint64, если верхней границы нет. ok = false, если нижней границы
// нет (>=, >, =) или мажоров в диапазоне нет вовсе. Нужна проверке «мажор
// диапазона cream: равен мажору схемы».
func (r Range) MajorBound() (lo, hi uint64, ok bool) {
	hasLower := false
	hi = math.MaxUint64
	for _, c := range r.cmps {
		m := c.ver.Major
		switch c.op {
		case opGE, opGT:
			hasLower = true
			lo = max(lo, m)
		case opEQ:
			hasLower = true
			lo = max(lo, m)
			hi = min(hi, m)
		case opLE:
			hi = min(hi, m)
		case opLT:
			if c.ver.Minor == 0 && c.ver.Patch == 0 && len(c.ver.Pre) == 0 {
				if m == 0 {
					return lo, 0, false
				}
				m--
			}
			hi = min(hi, m)
		}
	}
	if !hasLower || lo > hi {
		return lo, hi, false
	}
	return lo, hi, true
}

// AtLeast reports whether every version the range admits has its main
// version (pre-release dropped) at or above v's: some lower bound (>=, >, =)
// is itself at or above v. A manifest that uses a field of cream 1.1 must
// say so in cream: — ">=1.1.0" passes, ">=1.0.0" or "<2" do not.
func (r Range) AtLeast(v Version) bool {
	floor := Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch}
	for _, c := range r.cmps {
		switch c.op {
		case opGE, opGT, opEQ:
			core := Version{Major: c.ver.Major, Minor: c.ver.Minor, Patch: c.ver.Patch}
			if Compare(core, floor) >= 0 {
				return true
			}
		}
	}
	return false
}
