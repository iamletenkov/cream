// Package envfile reads and edits files in the systemd EnvironmentFile=
// grammar, keeping comments, blank lines and the order of everything it does
// not touch.
//
// The reader is a port of parse_env_file_internal() from systemd's
// src/basic/env-file.c (v254+): quotes are stripped, `\` escapes, `\<newline>`
// joins lines, `#` and `;` start comments, the last assignment wins and
// nothing is expanded. Names that are not valid environment names (including
// `export NAME`) are ignored, as systemd ignores them.
//
// Deliberate differences from systemd: a NUL byte is an error (systemd stops
// reading at it); invalid UTF-8 in an assignment is an error, as in systemd.
// Before v254 a comment ending in `\` swallowed the next line: Parse reads
// both dialects and refuses a file on which they disagree; the writer never
// produces such a comment.
package envfile

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"
)

type state int

const (
	preKey state = iota
	inKey
	preValue
	inValue
	valueEscape
	singleQuote
	doubleQuote
	doubleQuoteEscape
	comment
	commentEscape
)

// record is one assignment as systemd pushes it: raw text is
// src[start:end], end being the terminating newline or EOF.
type record struct {
	key, value string
	start, end int
}

// File is a parsed EnvironmentFile with pending edits.
type File struct {
	src     []byte
	recs    []record // valid names only, in file order
	set     map[string]string
	deleted map[string]bool
	added   []string // keys set but absent from recs, in Set order
}

// errDialect: systemd < 254 and v254+ read the file differently.
var errDialect = errors.New("файл читается systemd < 254 иначе: комментарий, оканчивающийся на \\, поглощает следующую строку")

// Parse reads data in the EnvironmentFile grammar of systemd v254+ and
// refuses a file that systemd < 254 (249, 252) reads into other assignments,
// so the result holds for every supported systemd.
func Parse(data []byte) (*File, error) {
	f, err := parseDialect(data, false)
	if err != nil {
		return nil, err
	}
	legacy, err := parseDialect(data, true)
	if err != nil {
		return nil, err
	}
	if n := min(len(f.recs), len(legacy.recs)); !slices.Equal(f.recs, legacy.recs) {
		i := 0
		for i < n && f.recs[i] == legacy.recs[i] {
			i++
		}
		at := len(data)
		if i < len(f.recs) {
			at = f.recs[i].start
		}
		if i < len(legacy.recs) {
			at = min(at, legacy.recs[i].start)
		}
		return nil, fmt.Errorf("строка %d: %w", 1+bytes.Count(data[:at], []byte{'\n'}), errDialect)
	}
	return f, nil
}

// parseDialect with legacy=true follows systemd < 254, where a comment ending
// in `\` swallows the next line.
func parseDialect(data []byte, legacy bool) (*File, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return nil, fmt.Errorf("байт NUL в позиции %d", i)
	}
	recs, _ := scan(data, legacy)
	f := &File{src: data, set: map[string]string{}, deleted: map[string]bool{}}
	for _, r := range recs {
		if !utf8.ValidString(r.key) || !utf8.ValidString(r.value) {
			return nil, fmt.Errorf("строка %d: недопустимый UTF-8", 1+bytes.Count(data[:r.start], []byte{'\n'}))
		}
		if ValidName(r.key) {
			f.recs = append(f.recs, r)
		}
	}
	return f, nil
}

func isNewline(c byte) bool    { return c == '\n' || c == '\r' }
func isWhitespace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// scan runs the systemd state machine over data and returns every pushed
// assignment (valid name or not) and the state at EOF.
func scan(data []byte, legacy bool) ([]record, state) {
	var (
		recs                 []record
		key, value           []byte
		lastKeyWS, lastValWS = -1, -1
		start                int
		st                   = preKey
	)
	push := func(end int, chompValue bool) {
		k := key
		if lastKeyWS >= 0 {
			k = k[:lastKeyWS]
		}
		v := value
		if chompValue && lastValWS >= 0 {
			v = v[:lastValWS]
		}
		recs = append(recs, record{key: string(k), value: string(v), start: start, end: end})
		key, value = key[:0], value[:0]
	}
	for i, c := range data {
		switch st {
		case preKey:
			switch {
			case c == '#' || c == ';':
				st = comment
			case !isWhitespace(c):
				st = inKey
				lastKeyWS = -1
				start = i
				key = append(key[:0], c)
			}
		case inKey:
			switch {
			case isNewline(c):
				st = preKey
				key = key[:0]
			case c == '=':
				st = preValue
				lastValWS = -1
				value = value[:0]
			default:
				if !isWhitespace(c) {
					lastKeyWS = -1
				} else if lastKeyWS < 0 {
					lastKeyWS = len(key)
				}
				key = append(key, c)
			}
		case preValue:
			switch {
			case isNewline(c):
				st = preKey
				push(i, false)
			case c == '\'':
				st = singleQuote
			case c == '"':
				st = doubleQuote
			case c == '\\':
				st = valueEscape
			case !isWhitespace(c):
				st = inValue
				value = append(value, c)
			}
		case inValue:
			switch {
			case isNewline(c):
				st = preKey
				push(i, true)
			case c == '\\':
				st = valueEscape
				lastValWS = -1
			default:
				if !isWhitespace(c) {
					lastValWS = -1
				} else if lastValWS < 0 {
					lastValWS = len(value)
				}
				value = append(value, c)
			}
		case valueEscape:
			st = inValue
			if !isNewline(c) {
				value = append(value, c)
			}
		case singleQuote:
			if c == '\'' {
				st = preValue
			} else {
				value = append(value, c)
			}
		case doubleQuote:
			switch c {
			case '"':
				st = preValue
			case '\\':
				st = doubleQuoteEscape
			default:
				value = append(value, c)
			}
		case doubleQuoteEscape:
			st = doubleQuote
			switch {
			case c == '"' || c == '\\' || c == '`' || c == '$':
				value = append(value, c)
			case c != '\n':
				value = append(value, '\\', c)
			}
		case comment:
			switch {
			case c == '\\':
				st = commentEscape
			case isNewline(c):
				st = preKey
			}
		case commentEscape:
			if legacy || !isNewline(c) {
				st = comment
			} else {
				st = preKey
			}
		}
	}
	switch st {
	case preValue, valueEscape, singleQuote, doubleQuote, doubleQuoteEscape:
		push(len(data), false)
	case inValue:
		push(len(data), true)
	case preKey, inKey, comment, commentEscape:
	}
	return recs, st
}

// Get returns the effective value of k: the last assignment wins.
func (f *File) Get(k string) (string, bool) {
	if f.deleted[k] {
		return "", false
	}
	if v, ok := f.set[k]; ok {
		return v, true
	}
	for i := len(f.recs) - 1; i >= 0; i-- {
		if f.recs[i].key == k {
			return f.recs[i].value, true
		}
	}
	return "", false
}

// Set assigns v to k. The first assignment of k is rewritten in place, later
// ones are dropped; a new key is appended at the end. k must satisfy
// ValidName and v must pass CheckValue: the caller validates input, a
// violation is a program bug and panics.
func (f *File) Set(k, v string) {
	if !ValidName(k) {
		panic(fmt.Sprintf("envfile: недопустимое имя %q", k))
	}
	if err := CheckValue(v); err != nil {
		panic(fmt.Sprintf("envfile: %s: %v", k, err))
	}
	delete(f.deleted, k)
	f.set[k] = v
	if !f.inRecs(k) && !slices.Contains(f.added, k) {
		f.added = append(f.added, k)
	}
}

// Delete removes every assignment of k.
func (f *File) Delete(k string) {
	delete(f.set, k)
	f.added = slices.DeleteFunc(f.added, func(x string) bool { return x == k })
	if f.inRecs(k) {
		f.deleted[k] = true
	}
}

// Keys returns the keys in effect, in order of first appearance.
func (f *File) Keys() []string {
	var keys []string
	seen := map[string]bool{}
	for _, r := range f.recs {
		if !seen[r.key] && !f.deleted[r.key] {
			seen[r.key] = true
			keys = append(keys, r.key)
		}
	}
	return append(keys, f.added...)
}

// Bytes renders the file: untouched text byte for byte, edited assignments
// as KEY=Quote(value), new keys appended one per line.
func (f *File) Bytes() []byte {
	var out bytes.Buffer
	pos := 0
	written := map[string]bool{}
	for _, r := range f.recs {
		v, isSet := f.set[r.key]
		switch {
		case f.deleted[r.key] || (isSet && written[r.key]):
			from := lineStart(f.src, pos, r.start)
			out.Write(f.src[pos:from])
			pos = skipTerminator(f.src, r.end)
		case isSet:
			written[r.key] = true
			out.Write(f.src[pos:r.start])
			out.WriteString(r.key + "=" + Quote(v))
			pos = r.end
		}
	}
	out.Write(f.src[pos:])
	if len(f.added) == 0 {
		return out.Bytes()
	}
	_, st := scan(out.Bytes(), false)
	out.WriteString(closeTail(out.Bytes(), st))
	for _, k := range f.added {
		out.WriteString(k + "=" + Quote(f.set[k]) + "\n")
	}
	return out.Bytes()
}

// closeTail returns what to write after b, scanned to state st, so that the
// next line starts a fresh assignment for systemd of any version while the
// last assignment of b keeps its value.
func closeTail(b []byte, st state) string {
	switch st {
	case singleQuote:
		return "'\n"
	case doubleQuote:
		return "\"\n"
	case doubleQuoteEscape:
		return "\n\"\n" // the escaped newline is eaten, then the quote closes
	case valueEscape, commentEscape:
		return "\n\n" // the first newline is eaten as a continuation
	case preKey, inKey, preValue, inValue, comment:
	}
	if len(b) == 0 || b[len(b)-1] == '\n' {
		return ""
	}
	return "\n"
}

// lineStart moves start back over spaces and tabs to the start of its line,
// not before floor, so a dropped assignment does not leave its indentation.
func lineStart(src []byte, floor, start int) int {
	i := start
	for i > floor && (src[i-1] == ' ' || src[i-1] == '\t') {
		i--
	}
	if i == 0 || isNewline(src[i-1]) {
		return i
	}
	return start
}

// skipTerminator returns the offset after the line terminator at end
// ("\n", "\r" or "\r\n"), or end itself at EOF.
func skipTerminator(src []byte, end int) int {
	if end < len(src) && src[end] == '\r' {
		end++
		if end < len(src) && src[end] == '\n' {
			end++
		}
		return end
	}
	if end < len(src) && src[end] == '\n' {
		end++
	}
	return end
}

func (f *File) inRecs(k string) bool {
	for _, r := range f.recs {
		if r.key == k {
			return true
		}
	}
	return false
}
