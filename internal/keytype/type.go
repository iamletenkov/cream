// Package keytype — типы значений ключей config.keys и параметров действий (§8.2 спеки)
// и описание ключа (Spec, Schema).
package keytype

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/iamletenkov/cream/internal/envfile"
)

// Kind — вид типа.
type Kind string

// Виды типов §8.2.
const (
	KindBool Kind = "bool" // "0" или "1", как в knobs bisquite
	KindInt  Kind = "int"  // десятичное int64 без "+", ведущих нулей и "-0"
	KindPort Kind = "port" // 1–65535
	KindHost Kind = "host" // рулинг Р10: DNS-имя (LDH), IPv4 или IPv6, необязательный :port
	KindPath Kind = "path" // абсолютный путь без управляющих знаков
	KindStr  Kind = "str"  // без управляющих знаков, $ и обратных кавычек
	KindEnum Kind = "enum" // enum:a,b
	KindRe   Kind = "re"   // re:<RE2>, значение сверяется целиком
	KindList Kind = "list" // list:<тип>, элементы через пробел
)

// Type — разобранный тип ключа. Enum — для enum, Re — для re, Elem — для list;
// Raw — исходная запись.
type Type struct {
	Kind Kind
	Enum []string
	Re   *regexp.Regexp
	Elem *Type
	Raw  string
}

// ParseType разбирает запись типа: bool int port host path str enum:a,b re:<RE2> list:<тип>.
// list:list:… запрещён: элементы списка не содержат пробелов.
func ParseType(s string) (Type, error) {
	t, err := parseType(s)
	if err != nil {
		return Type{}, fmt.Errorf("тип %q: %w", s, err)
	}
	return t, nil
}

func parseType(s string) (Type, error) {
	switch Kind(s) {
	case KindBool, KindInt, KindPort, KindHost, KindPath, KindStr:
		return Type{Kind: Kind(s), Raw: s}, nil
	}
	name, arg, ok := strings.Cut(s, ":")
	if !ok {
		return Type{}, errors.New("неизвестный тип")
	}
	switch Kind(name) {
	case KindEnum:
		items := strings.Split(arg, ",")
		for i, it := range items {
			if it == "" {
				return Type{}, errors.New("пустое значение в enum")
			}
			if strings.ContainsFunc(it, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
				return Type{}, fmt.Errorf("значение enum %q с пробелом или управляющим знаком", it)
			}
			if slices.Contains(items[:i], it) {
				return Type{}, fmt.Errorf("значение enum %q повторяется", it)
			}
		}
		return Type{Kind: KindEnum, Enum: items, Raw: s}, nil
	case KindRe:
		if arg == "" {
			return Type{}, errors.New("пустое регулярное выражение")
		}
		// Компиляция RE2 отвергает обратные ссылки и lookaround — синтаксис ERE/PCRE вне RE2.
		// Отдельная компиляция без обёртки нужна ещё и затем, чтобы "a)|(b" не прошло,
		// склеившись с ^(?:…)$ в другое выражение.
		if _, err := regexp.Compile(arg); err != nil {
			return Type{}, fmt.Errorf("выражение вне RE2: %w", err)
		}
		re, err := regexp.Compile(`^(?:` + arg + `)$`)
		if err != nil {
			return Type{}, fmt.Errorf("выражение вне RE2: %w", err)
		}
		return Type{Kind: KindRe, Re: re, Raw: s}, nil
	case KindList:
		if arg == "" {
			return Type{}, errors.New("у list: нет типа элемента")
		}
		elem, err := parseType(arg)
		if err != nil {
			return Type{}, err
		}
		if elem.Kind == KindList {
			return Type{}, errors.New("вложенный list:list: не поддерживается")
		}
		return Type{Kind: KindList, Elem: &elem, Raw: s}, nil
	}
	return Type{}, errors.New("неизвестный тип")
}

// Check проверяет значение по типу. Пустую строку принимают только str, list
// (пустой список) и re, если выражение её допускает; обязательность пустого
// значения решает Spec.CheckValue.
//
// Недопустимый UTF-8 и NUL Check отвергает для типа любого вида: это граница,
// за которой значение пишется в EnvironmentFile, и envfile.Set паникует на
// том, что не прошло бы envfile.CheckValue.
func (t Type) Check(v string) error {
	err := envfile.CheckValue(v)
	if err == nil {
		err = t.check(v)
	}
	if err != nil {
		return fmt.Errorf("значение не подходит под тип %s: %w", t.Raw, err)
	}
	return nil
}

func (t Type) check(v string) error {
	switch t.Kind {
	case KindBool:
		if v != "0" && v != "1" {
			return errors.New("ожидается 0 или 1")
		}
	case KindInt:
		if !isDecimal(v, true) {
			return errors.New("ожидается целое число")
		}
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return errors.New("число вне диапазона int64")
		}
	case KindPort:
		return checkPort(v)
	case KindHost:
		return checkHost(v)
	case KindPath:
		if !strings.HasPrefix(v, "/") {
			return errors.New("ожидается абсолютный путь")
		}
		if strings.ContainsFunc(v, unicode.IsControl) {
			return errors.New("управляющий знак в пути")
		}
	case KindStr:
		return checkStr(v)
	case KindEnum:
		if !slices.Contains(t.Enum, v) {
			return fmt.Errorf("ожидается одно из: %s", strings.Join(t.Enum, ", "))
		}
	case KindRe:
		if !t.Re.MatchString(v) {
			return errors.New("не совпадает с выражением")
		}
	case KindList:
		if strings.ContainsFunc(v, unicode.IsControl) {
			return errors.New("управляющий знак в списке (элементы — через пробел)")
		}
		for _, el := range strings.Split(v, " ") {
			if el == "" {
				continue
			}
			if err := t.Elem.check(el); err != nil {
				return fmt.Errorf("элемент %q: %w", el, err)
			}
		}
	default:
		return errors.New("тип не разобран")
	}
	return nil
}

func checkStr(v string) error {
	for _, r := range v {
		switch {
		case unicode.IsControl(r):
			return errors.New("управляющий знак")
		case r == '$':
			return errors.New("знак $ запрещён")
		case r == '`':
			return errors.New("обратная кавычка запрещена")
		}
	}
	return nil
}

// isDecimal: 0|-?[1-9][0-9]* (без "-0").
func isDecimal(v string, signed bool) bool {
	if signed {
		var neg bool
		v, neg = strings.CutPrefix(v, "-")
		if neg && v == "0" {
			return false
		}
	}
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return v == "0" || v[0] != '0'
}

func checkPort(v string) error {
	if !isDecimal(v, false) {
		return errors.New("ожидается номер порта 1–65535")
	}
	n, err := strconv.ParseUint(v, 10, 16)
	if err != nil || n == 0 {
		return errors.New("ожидается номер порта 1–65535")
	}
	return nil
}

// checkHost — рулинг Р10: имя (LDH, ASCII), IPv4 или IPv6, необязательный :port;
// IPv6 с портом — в квадратных скобках.
func checkHost(v string) error {
	if v == "" {
		return errors.New("пустой адрес")
	}
	if rest, ok := strings.CutPrefix(v, "["); ok {
		addr, tail, found := strings.Cut(rest, "]")
		if !found {
			return errors.New("нет закрывающей скобки")
		}
		if err := checkIPv6(addr); err != nil {
			return err
		}
		if tail == "" {
			return nil
		}
		port, ok := strings.CutPrefix(tail, ":")
		if !ok {
			return errors.New("после ] ожидается :порт")
		}
		return checkPort(port)
	}
	if strings.Count(v, ":") > 1 {
		return checkIPv6(v)
	}
	host, port, hasPort := strings.Cut(v, ":")
	if hasPort {
		if err := checkPort(port); err != nil {
			return err
		}
	}
	if a, err := netip.ParseAddr(host); err == nil && a.Is4() {
		return nil
	}
	return checkDNSName(host)
}

func checkIPv6(s string) error {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is6() || a.Zone() != "" {
		return fmt.Errorf("%q — не адрес IPv6", s)
	}
	return nil
}

func checkDNSName(h string) error {
	if h == "" || len(h) > 253 {
		return errors.New("имя хоста пустое или длиннее 253 знаков")
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 {
			return fmt.Errorf("метка имени %q пустая или длиннее 63 знаков", l)
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return fmt.Errorf("метка имени %q начинается или кончается дефисом", l)
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !isAlnumHyphen(rune(c)) {
				return fmt.Errorf("недопустимый знак в имени %q (только ASCII-буквы, цифры, дефис; IDN — в punycode)", h)
			}
		}
	}
	// Последняя метка из одних цифр — это не имя, а неверный IPv4 ("256.1.1.1").
	if allDigitsASCII(labels[len(labels)-1]) {
		return fmt.Errorf("%q — не имя хоста и не IPv4", h)
	}
	return nil
}

func allDigitsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isAlnumHyphen: ASCII-буква, цифра или дефис.
func isAlnumHyphen(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-'
}
