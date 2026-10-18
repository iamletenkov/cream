package keytype

import "testing"

func mustType(t *testing.T, s string) Type {
	t.Helper()
	tp, err := ParseType(s)
	if err != nil {
		t.Fatalf("ParseType(%q): %v", s, err)
	}
	return tp
}

func TestSpecCheckValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		typ   string
		empty bool
		v     string
		ok    bool
	}{
		{name: "пустое без empty", typ: "str", v: "", ok: false},
		{name: "пустое с empty", typ: "str", empty: true, v: "", ok: true},
		{name: "пустой host с empty", typ: "host", empty: true, v: "", ok: true},
		{name: "пустой host без empty", typ: "host", v: "", ok: false},
		{name: "пустой список без empty", typ: "list:str", v: "", ok: false},
		{name: "пустой список с empty", typ: "list:str", empty: true, v: "", ok: true},
		{name: "непустое по типу", typ: "port", empty: true, v: "443", ok: true},
		{name: "непустое против типа", typ: "port", empty: true, v: "x", ok: false},
		{name: "пробелы — не пустое", typ: "list:str", empty: true, v: "  ", ok: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := Spec{Type: mustType(t, tc.typ), Empty: tc.empty, Description: "x"}
			err := s.CheckValue(tc.v)
			if tc.ok != (err == nil) {
				t.Fatalf("CheckValue(%q) = %v, ждали ok=%v", tc.v, err, tc.ok)
			}
		})
	}
}

func TestSchemaLookup(t *testing.T) {
	t.Parallel()
	s := Schema{
		"TELEPORT_PROXY": {Type: mustType(t, "host"), Description: "точный"},
		"TELEPORT_*":     {Type: mustType(t, "str"), Pattern: true, Description: "шаблон"},
		"TELEPORT_LBL_*": {Type: mustType(t, "re:^[a-z]+$"), Pattern: true, Description: "длинный шаблон"},
		"LABEL_X":        {Type: mustType(t, "str"), Description: "точный другой"},
		"NOTPATTERN_*":   {Type: mustType(t, "str"), Description: "звёздочка без pattern"},
	}
	tests := []struct {
		key  string
		desc string
		ok   bool
	}{
		{key: "TELEPORT_PROXY", desc: "точный", ok: true}, // точное имя важнее шаблона
		{key: "TELEPORT_ENV", desc: "шаблон", ok: true},
		{key: "TELEPORT_LBL_A", desc: "длинный шаблон", ok: true}, // самый длинный префикс
		{key: "LABEL_X", desc: "точный другой", ok: true},
		{key: "TELEPORT_", ok: false}, // пустой хвост шаблона
		{key: "LABEL_Y", ok: false},
		{key: "NOTPATTERN_A", ok: false},
		{key: "teleport_env", ok: false},
		{key: "", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			got, ok := s.Lookup(tc.key)
			if ok != tc.ok {
				t.Fatalf("Lookup(%q) ok = %v, ждали %v", tc.key, ok, tc.ok)
			}
			if ok && got.Description != tc.desc {
				t.Fatalf("Lookup(%q) = %q, ждали %q", tc.key, got.Description, tc.desc)
			}
		})
	}
}
