package envfile

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Quote returns v in the form Parse (and systemd) reads back byte for byte:
// bare when it consists only of [A-Za-z0-9_./:,@%+=-], otherwise in double
// quotes with `\` and `"` escaped. Newlines and other bytes inside double
// quotes are kept literally.
func Quote(v string) string {
	if isBare(v) {
		return v
	}
	var b strings.Builder
	b.Grow(len(v) + 2)
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		if c := v[i]; c == '\\' || c == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(v[i])
	}
	b.WriteByte('"')
	return b.String()
}

func isBare(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("_./:,@%+=-", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// ValidName reports whether k is a valid environment name for systemd:
// [A-Za-z_][A-Za-z0-9_]*.
func ValidName(k string) bool {
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		return false
	}
	for i := 0; i < len(k); i++ {
		switch c := k[i]; {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// CheckValue reports whether v can be stored: no NUL byte, valid UTF-8.
// Anything else (newlines, quotes, `$`) is representable via Quote.
func CheckValue(v string) error {
	if strings.IndexByte(v, 0) >= 0 {
		return errors.New("значение содержит байт NUL")
	}
	if !utf8.ValidString(v) {
		return errors.New("значение — недопустимый UTF-8")
	}
	return nil
}
