package keytype

import (
	"strings"
	"testing"
)

func TestParseType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		kind Kind
		err  bool
	}{
		{in: "bool", kind: KindBool},
		{in: "int", kind: KindInt},
		{in: "port", kind: KindPort},
		{in: "host", kind: KindHost},
		{in: "path", kind: KindPath},
		{in: "str", kind: KindStr},
		{in: "enum:a,b", kind: KindEnum},
		{in: "enum:only", kind: KindEnum},
		{in: "re:^[a-z]+$", kind: KindRe},
		{in: "list:str", kind: KindList},
		{in: "list:re:^[a-z]+=[0-9]+$", kind: KindList},
		{in: "list:enum:a,b", kind: KindList},
		{in: "", err: true},
		{in: "string", err: true},
		{in: "Bool", err: true},
		{in: "secret", err: true},
		{in: "enum:", err: true},
		{in: "enum:a,,b", err: true},
		{in: "enum:a,a", err: true},
		{in: "enum:a b", err: true},
		{in: "re:", err: true},
		{in: `re:(a)\1`, err: true}, // обратная ссылка — вне RE2
		{in: "re:(?=a)", err: true}, // lookahead — вне RE2
		{in: "re:[a-", err: true},
		{in: "list:", err: true},
		{in: "list:list:str", err: true},
		{in: "list:nope", err: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseType(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("ParseType(%q) = %+v, ждали ошибку", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseType(%q): %v", tc.in, err)
			}
			if got.Kind != tc.kind {
				t.Fatalf("Kind = %q, ждали %q", got.Kind, tc.kind)
			}
			if got.Raw != tc.in {
				t.Fatalf("Raw = %q, ждали %q", got.Raw, tc.in)
			}
		})
	}
}

func TestParseTypeParts(t *testing.T) {
	t.Parallel()
	e, err := ParseType("enum:a,b")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(e.Enum, "|") != "a|b" {
		t.Fatalf("Enum = %v", e.Enum)
	}
	l, err := ParseType("list:port")
	if err != nil {
		t.Fatal(err)
	}
	if l.Elem == nil || l.Elem.Kind != KindPort || l.Elem.Raw != "port" {
		t.Fatalf("Elem = %+v", l.Elem)
	}
	r, err := ParseType("re:^a+$")
	if err != nil {
		t.Fatal(err)
	}
	if r.Re == nil {
		t.Fatal("Re = nil")
	}
}

type checkCase struct {
	v  string
	ok bool
}

func runChecks(t *testing.T, typ string, cases []checkCase) {
	t.Helper()
	tp, err := ParseType(typ)
	if err != nil {
		t.Fatalf("ParseType(%q): %v", typ, err)
	}
	for _, tc := range cases {
		t.Run(typ+"/"+tc.v, func(t *testing.T) {
			t.Parallel()
			err := tp.Check(tc.v)
			if tc.ok && err != nil {
				t.Fatalf("%s.Check(%q): %v", typ, tc.v, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("%s.Check(%q) принял, ждали отказ", typ, tc.v)
			}
		})
	}
}

func TestCheckBool(t *testing.T) {
	t.Parallel()
	runChecks(t, "bool", []checkCase{
		{"0", true}, {"1", true},
		{"", false}, {"2", false}, {"true", false}, {"yes", false}, {" 1", false}, {"01", false},
	})
}

func TestCheckInt(t *testing.T) {
	t.Parallel()
	runChecks(t, "int", []checkCase{
		{"0", true}, {"42", true}, {"-7", true},
		{"9223372036854775807", true}, {"-9223372036854775808", true},
		{"9223372036854775808", false}, {"", false}, {"+1", false}, {"007", false},
		{"1.5", false}, {"1e3", false}, {"0x10", false}, {"-", false}, {"1_000", false}, {" 1", false},
		{"-0", false}, {"-00", false},
	})
}

func TestCheckPort(t *testing.T) {
	t.Parallel()
	runChecks(t, "port", []checkCase{
		{"1", true}, {"443", true}, {"65535", true},
		{"0", false}, {"65536", false}, {"-1", false}, {"", false}, {"0443", false}, {"80a", false},
	})
}

func TestCheckHost(t *testing.T) {
	t.Parallel()
	runChecks(t, "host", []checkCase{
		{"localhost", true},
		{"proxy.example", true},
		{"proxy.example:443", true},
		{"a-b.c0.example.com", true},
		{"xn--e1afmkfd.xn--p1ai", true}, // punycode — ASCII
		{"192.168.0.1", true},
		{"192.168.0.1:3080", true},
		{"::1", true},
		{"2001:db8::1", true},
		{"[2001:db8::1]:443", true},
		{"[::1]", true},
		{"proxy.example:1", true},
		{"proxy.example:65535", true},
		{"", false},
		{"пример.рф", false},           // IDN — отказ
		{"proxy.example:0", false},     // порт 0
		{"proxy.example:65536", false}, // порт вне диапазона
		{"proxy.example:", false},
		{"proxy.example:0443", false},
		{"2001:db8::1:443:x", false},
		{"[2001:db8::1]:", false},
		{"[2001:db8::1]x", false},
		{"[192.168.0.1]:443", false}, // в скобках только IPv6
		{"[proxy.example]:443", false},
		{"fe80::1%eth0", false}, // зона — отказ
		{"-bad.example", false},
		{"bad-.example", false},
		{"bad..example", false},
		{"bad_name.example", false},
		{".example", false},
		{"example.", false},
		{"256.1.1.1", false}, // похоже на IPv4, но нет
		{"192.168.0", false}, // последний метка — число
		{"01.2.3.4", false},  // ведущий ноль
		{"a b", false},
		{"http://x", false},
		{strings.Repeat("a", 64) + ".example", false},
		{strings.Repeat("a", 63) + ".example", true},
		{strings.Repeat(strings.Repeat("a", 63)+".", 4) + "ab", false}, // > 253
	})
}

func TestCheckPath(t *testing.T) {
	t.Parallel()
	runChecks(t, "path", []checkCase{
		{"/", true}, {"/opt/teleport/teleport.tar.gz", true}, {"/a b/c", true},
		{"", false}, {"relative/x", false}, {"./x", false}, {"/a\nb", false}, {"/a\x00b", false}, {"/a\tb", false},
	})
}

func TestCheckStr(t *testing.T) {
	t.Parallel()
	runChecks(t, "str", []checkCase{
		{"hello world", true}, {"", true}, {"привет", true}, {`a"b'c\d`, true}, {"https://mirror.example/x", true},
		{"a$b", false}, {"a`b`", false}, {"a\nb", false}, {"a\tb", false}, {"a\x7fb", false}, {"a\rb", false},
		{"\x00", false}, {"a\u0085b", false},
	})
}

func TestCheckEnum(t *testing.T) {
	t.Parallel()
	runChecks(t, "enum:a,b", []checkCase{
		{"a", true}, {"b", true}, {"", false}, {"c", false}, {"a,b", false}, {"A", false},
	})
}

func TestCheckRe(t *testing.T) {
	t.Parallel()
	runChecks(t, "re:^[0-9a-f]{4}$", []checkCase{
		{"00ff", true}, {"00f", false}, {"00ff0", false}, {"", false},
	})
	// Без якорей выражение всё равно сравнивается со всем значением.
	runChecks(t, "re:[a-z]+", []checkCase{
		{"abc", true}, {"abc1", false}, {"1abc", false},
	})
	runChecks(t, "re:a|b", []checkCase{
		{"a", true}, {"b", true}, {"ab", false},
	})
}

func TestCheckList(t *testing.T) {
	t.Parallel()
	runChecks(t, "list:re:^[a-z]+=[0-9]+$", []checkCase{
		{"", true}, {"a=1", true}, {"a=1 b=2", true}, {"a=1  b=2", true}, {" a=1 ", true},
		{"a=1 B=2", false}, {"a=1\nb=2", false}, {"a=1\tb=2", false},
	})
	runChecks(t, "list:port", []checkCase{
		{"22 443", true}, {"22 0", false},
	})
	runChecks(t, "list:enum:x,y", []checkCase{
		{"x y x", true}, {"x z", false},
	})
}

// Check — граница, за которой envfile.Set не паникует: недопустимый UTF-8 и
// NUL отвергает тип любого вида, включая re и элементы list.
func TestCheckRejectsInvalidUTF8AndNUL(t *testing.T) {
	t.Parallel()
	bad := []checkCase{{"\xff", false}, {"a\xc3", false}, {"a\x00b", false}, {"\x00", false}}
	for _, typ := range []string{"str", "path", "re:.*", "re:(?s).*", "re:[^a]*", "list:re:.*", "list:str", "list:path", "bool", "int", "port", "host"} {
		cases := bad
		if strings.HasPrefix(typ, "path") || typ == "list:path" {
			cases = []checkCase{{"/\xff", false}, {"/a\xc3", false}, {"/a\x00b", false}}
		}
		runChecks(t, typ, cases)
	}
	runChecks(t, "list:re:.*", []checkCase{{"a \xff", false}, {"a b", true}})
	runChecks(t, "re:.*", []checkCase{{"любое", true}})
}

func TestCheckEnumRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()
	tp := Type{Kind: KindEnum, Enum: []string{"\xff", "a\x00"}, Raw: "enum:?"}
	for _, v := range []string{"\xff", "a\x00"} {
		if err := tp.Check(v); err == nil {
			t.Errorf("enum.Check(%q) принял, ждали отказ", v)
		}
	}
}

func TestParseTypeReNoWrapEscape(t *testing.T) {
	t.Parallel()
	if _, err := ParseType("re:a)|(b"); err == nil {
		t.Fatal("re:a)|(b принят: выражение вышло за обёртку ^(?:…)$")
	}
}
