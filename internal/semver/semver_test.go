package semver

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want Version
		err  bool
	}{
		{name: "простая", in: "1.2.3", want: Version{Major: 1, Minor: 2, Patch: 3}},
		{name: "нули", in: "0.0.0", want: Version{}},
		{name: "pre", in: "1.0.0-rc.1", want: Version{Major: 1, Pre: []string{"rc", "1"}}},
		{name: "pre с дефисом", in: "1.0.0-x-y.0", want: Version{Major: 1, Pre: []string{"x-y", "0"}}},
		{name: "буквенно-цифровой с нулём", in: "1.0.0-0a", want: Version{Major: 1, Pre: []string{"0a"}}},
		{name: "большое число", in: "18446744073709551615.0.0", want: Version{Major: 18446744073709551615}},
		{name: "переполнение", in: "18446744073709551616.0.0", err: true},
		{name: "ведущий ноль мажора", in: "01.2.3", err: true},
		{name: "ведущий ноль патча", in: "1.2.03", err: true},
		{name: "ведущий ноль в числовом pre", in: "1.2.3-01", err: true},
		{name: "пустой pre", in: "1.2.3-", err: true},
		{name: "пустой идентификатор pre", in: "1.2.3-a..b", err: true},
		{name: "недопустимый знак в pre", in: "1.2.3-a_b", err: true},
		{name: "префикс v", in: "v1.2.3", err: true},
		{name: "метаданные сборки", in: "1.2.3+build", err: true},
		{name: "два компонента", in: "1.2", err: true},
		{name: "четыре компонента", in: "1.2.3.4", err: true},
		{name: "пусто", in: "", err: true},
		{name: "пробел", in: " 1.2.3", err: true},
		{name: "знак", in: "+1.2.3", err: true},
		{name: "пустой компонент", in: "1..3", err: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("Parse(%q) = %v, ждали ошибку", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.in, err)
			}
			if Compare(got, tc.want) != 0 || !slices.Equal(got.Pre, tc.want.Pre) {
				t.Fatalf("Parse(%q) = %#v, ждали %#v", tc.in, got, tc.want)
			}
			if got.String() != tc.in {
				t.Fatalf("String() = %q, ждали %q", got.String(), tc.in)
			}
		})
	}
}

func TestParseLoose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
		err  bool
	}{
		{in: "12", want: "12.0.0"},
		{in: "13", want: "13.0.0"},
		{in: "22.04", want: "22.4.0"},
		{in: "24.04.1", want: "24.4.1"},
		{in: "1.2.3-rc.1", want: "1.2.3-rc.1"},
		{in: "5-rc", want: "5.0.0-rc"},
		{in: "", err: true},
		{in: "v12", err: true},
		{in: "12.", err: true},
		{in: "1.2.3.4", err: true},
		{in: "bookworm", err: true},
		{in: "1.2.3+b", err: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLoose(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("ParseLoose(%q) = %v, ждали ошибку", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLoose(%q): %v", tc.in, err)
			}
			if got.String() != tc.want {
				t.Fatalf("ParseLoose(%q) = %q, ждали %q", tc.in, got.String(), tc.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()
	// Порядок из semver 2.0 §11 плюс числовые компоненты.
	ordered := []string{
		"0.9.9",
		"1.0.0-0",
		"1.0.0-2",
		"1.0.0-10",
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
		"1.0.1",
		"1.1.0",
		"1.10.0",
		"2.0.0",
		"10.0.0",
	}
	vs := make([]Version, len(ordered))
	for i, s := range ordered {
		v, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		vs[i] = v
	}
	for i := range vs {
		for j := range vs {
			want := 0
			switch {
			case i < j:
				want = -1
			case i > j:
				want = 1
			}
			if got := Compare(vs[i], vs[j]); got != want {
				t.Errorf("Compare(%s, %s) = %d, ждали %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}
