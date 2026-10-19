package semver

import (
	"math"
	"testing"
)

func TestParseRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string // каноническая запись String()
		err  bool
	}{
		{in: ">=1.0.0", want: ">=1.0.0"},
		{in: ">=4.0.0 <5", want: ">=4.0.0 <5"},
		{in: "  >=4.0.0   <5  ", want: ">=4.0.0 <5"},
		{in: "<5", want: "<5"},
		{in: "=1.2.3", want: "=1.2.3"},
		{in: ">=12", want: ">=12"},
		{in: ">=22.04", want: ">=22.04"},
		{in: ">= 1.0.0", want: ">=1.0.0"},
		{in: ">1.0.0 <=2.0.0-rc.1", want: ">1.0.0 <=2.0.0-rc.1"},
		{in: ">=1.0.0 || >=2.0.0", err: true},
		{in: ">=1.0.0||<0.5", err: true},
		{in: "", err: true},
		{in: "   ", err: true},
		{in: "1.2.3", err: true},
		{in: "~1.2", err: true},
		{in: "^1.2.3", err: true},
		{in: ">=", err: true},
		{in: ">=1.0.0 <", err: true},
		{in: ">=x", err: true},
		{in: "=>1.0.0", err: true},
		{in: ">=1.0.0,<2", err: true},
		{in: "1.0.0 - 2.0.0", err: true},
		{in: ">=v1", err: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRange(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("ParseRange(%q) = %q, ждали ошибку", tc.in, got.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRange(%q): %v", tc.in, err)
			}
			if got.String() != tc.want {
				t.Fatalf("String() = %q, ждали %q", got.String(), tc.want)
			}
		})
	}
}

func TestRangeContains(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rng  string
		ver  string
		want bool
	}{
		{">=1.0.0", "1.0.0", true},
		{">=1.0.0", "0.9.9", false},
		{">=1.0.0", "1.0.0-rc.1", false},
		{">=1.0.0", "7.3.1", true},
		{">=4.0.0 <5", "4.0.0", true},
		{">=4.0.0 <5", "4.99.99", true},
		{">=4.0.0 <5", "5.0.0", false},
		// Рулинг: "<X" и "<X.Y.Z" исключают предрелизы самой границы ("<5" ≡ "<5.0.0-0"),
		// как MajorBound считает "<5" мажором 4.
		{">=4.0.0 <5", "5.0.0-rc.1", false},
		{">=1.0.0 <2", "2.0.0-rc.1", false},
		{">=1.0.0 <2", "1.9.0", true},
		{">=1.0.0 <2", "1.9.0-rc.1", true},
		{"<1.2.3", "1.2.3-rc.1", false},
		{"<1.2.3", "1.2.2", true},
		{"<1.2.3-rc.2", "1.2.3-rc.1", true}, // граница с предрелизом — обычное сравнение
		{"<1.2.3-rc.2", "1.2.3-rc.2", false},
		{">=4.0.0 <5", "3.9.9", false},
		{"<5", "0.0.0", true},
		{"<5", "5.0.0", false},
		{"=1.2.3", "1.2.3", true},
		{"=1.2.3", "1.2.4", false},
		{"=1.2.3", "1.2.3-rc", false},
		{">1.2.3", "1.2.3", false},
		{">1.2.3", "1.2.4", true},
		{"<=1.2.3", "1.2.3", true},
		{"<=1.2.3", "1.2.4", false},
		{">=12", "12.0.0", true},
		{">=12", "11.9.0", false},
		{">=22.04", "22.4.0", true},
		{">=22.04", "22.3.9", false},
		{">=22.04", "24.4.0", true},
		{">=2 <1", "1.5.0", false}, // пустой диапазон
	}
	for _, tc := range tests {
		t.Run(tc.rng+" "+tc.ver, func(t *testing.T) {
			t.Parallel()
			r, err := ParseRange(tc.rng)
			if err != nil {
				t.Fatalf("ParseRange(%q): %v", tc.rng, err)
			}
			v, err := Parse(tc.ver)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.ver, err)
			}
			if got := r.Contains(v); got != tc.want {
				t.Fatalf("%q.Contains(%s) = %v, ждали %v", tc.rng, tc.ver, got, tc.want)
			}
		})
	}
}

func TestRangeMajorBound(t *testing.T) {
	t.Parallel()
	const inf = math.MaxUint64
	tests := []struct {
		rng    string
		lo, hi uint64
		ok     bool
	}{
		{rng: ">=1.0.0", lo: 1, hi: inf, ok: true},
		{rng: ">=1.0.0 <2", lo: 1, hi: 1, ok: true},
		{rng: ">=1.2.0 <2.0.0", lo: 1, hi: 1, ok: true},
		{rng: ">=1.0.0 <2.1", lo: 1, hi: 2, ok: true},
		{rng: ">=1.0.0 <=2.0.0", lo: 1, hi: 2, ok: true},
		{rng: ">=1.0.0 <2.0.0-rc.1", lo: 1, hi: 2, ok: true},
		{rng: ">1.9.9", lo: 1, hi: inf, ok: true},
		{rng: "=1.2.3", lo: 1, hi: 1, ok: true},
		{rng: ">=4.0.0 <5", lo: 4, hi: 4, ok: true},
		{rng: ">=1.0.0 >=3.0.0 <9 <4", lo: 3, hi: 3, ok: true},
		{rng: "<5", ok: false},                           // нет нижней границы
		{rng: ">=3.0.0 <2", ok: false},                   // пустой по мажорам
		{rng: ">=1.0.0 <1.0.0", lo: 1, hi: 0, ok: false}, // <1.0.0 — максимум мажор 0
	}
	for _, tc := range tests {
		t.Run(tc.rng, func(t *testing.T) {
			t.Parallel()
			r, err := ParseRange(tc.rng)
			if err != nil {
				t.Fatalf("ParseRange(%q): %v", tc.rng, err)
			}
			lo, hi, ok := r.MajorBound()
			if ok != tc.ok {
				t.Fatalf("MajorBound(%q) ok = %v, ждали %v (lo=%d hi=%d)", tc.rng, ok, tc.ok, lo, hi)
			}
			if ok && (lo != tc.lo || hi != tc.hi) {
				t.Fatalf("MajorBound(%q) = %d, %d, ждали %d, %d", tc.rng, lo, hi, tc.lo, tc.hi)
			}
		})
	}
}

func TestRangeAtLeast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rng, min string
		want     bool
	}{
		{">=1.1.0", "1.1.0", true},
		{">=1.2.0 <2", "1.1.0", true},
		{"=1.1.0", "1.1.0", true},
		{">1.1.0", "1.1.0", true},
		{">=1.1.0-rc.1", "1.1.0", true}, // предрелиз сравнивается по основной версии
		{">=1.0.0", "1.1.0", false},
		{">=1.0.5 <2", "1.1.0", false},
		{">1.0.99", "1.1.0", false}, // допускает 1.0.100
		{"<2", "1.1.0", false},      // нет нижней границы
		{">=1.0.0 >=1.1.0", "1.1.0", true},
	}
	for _, tc := range tests {
		t.Run(tc.rng+" "+tc.min, func(t *testing.T) {
			t.Parallel()
			r, err := ParseRange(tc.rng)
			if err != nil {
				t.Fatal(err)
			}
			if got := r.AtLeast(mustParse(t, tc.min)); got != tc.want {
				t.Errorf("AtLeast(%q, %q) = %v, want %v", tc.rng, tc.min, got, tc.want)
			}
		})
	}
}

func mustParse(t *testing.T, s string) Version {
	t.Helper()
	v, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
