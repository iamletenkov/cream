package cerr

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
)

func TestExitCode(t *testing.T) {
	t.Parallel()
	base := New(ExitNotFound, "not_found.ext", "расширение %q не найдено", "teleport")
	tests := []struct {
		name     string
		err      error
		wantExit int
		wantCode string
	}{
		{name: "nil is ok", err: nil, wantExit: ExitOK, wantCode: ""},
		{name: "cerr as is", err: base, wantExit: ExitNotFound, wantCode: "not_found.ext"},
		{name: "wrapped by fmt", err: fmt.Errorf("установка: %w", base), wantExit: ExitNotFound, wantCode: "not_found.ext"},
		{name: "double wrapped", err: fmt.Errorf("a: %w", fmt.Errorf("b: %w", base)), wantExit: ExitNotFound, wantCode: "not_found.ext"},
		{name: "joined", err: errors.Join(errors.New("x"), base), wantExit: ExitNotFound, wantCode: "not_found.ext"},
		{name: "plain error is internal", err: errors.New("boom"), wantExit: ExitInternal, wantCode: "internal"},
		{name: "cerr wraps cerr — outer wins", err: Wrap(base, ExitGraph, "deps.missing", "зависимость"), wantExit: ExitGraph, wantCode: "deps.missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ExitCode(tc.err); got != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d", got, tc.wantExit)
			}
			if got := CodeOf(tc.err); got != tc.wantCode {
				t.Errorf("CodeOf = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

func TestWrapUnwrapAndMessage(t *testing.T) {
	t.Parallel()
	e := Wrap(fs.ErrNotExist, ExitState, "state.read", "чтение реестра %s", "teleport")
	if !errors.Is(e, fs.ErrNotExist) {
		t.Errorf("errors.Is(fs.ErrNotExist) = false, want true")
	}
	if got, want := e.Error(), "чтение реестра teleport: file does not exist"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got, want := New(ExitUsage, "usage.args", "нет аргумента").Error(), "нет аргумента"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	var target *Error
	if !errors.As(fmt.Errorf("x: %w", e), &target) || target.Code != "state.read" {
		t.Errorf("errors.As did not find *Error with code state.read")
	}
}

func TestExitCodesMatchSpec(t *testing.T) {
	t.Parallel()
	// §11.3: коды — внешний контракт, их значения не меняются.
	got := []int{ExitOK, ExitCheck, ExitUsage, ExitData, ExitNotFound, ExitGraph, ExitNotApplicable,
		ExitSource, ExitInternal, ExitState, ExitLock, ExitProtocol, ExitPerm, ExitHook}
	want := []int{0, 1, 64, 65, 66, 67, 68, 69, 70, 74, 75, 76, 77, 79}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("code #%d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestWarnings(t *testing.T) {
	t.Parallel()
	var w Warnings
	if got := w.List(); len(got) != 0 {
		t.Fatalf("zero Warnings List() = %v, want empty", got)
	}
	w.Add("source.shadowed", "источник %s перекрыт %s", "a", "b")
	w.Add("x.y", "без аргументов")
	got := w.List()
	want := []Warning{{Code: "source.shadowed", Message: "источник a перекрыт b"}, {Code: "x.y", Message: "без аргументов"}}
	if len(got) != len(want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	got[0].Code = "mutated"
	if w.List()[0].Code != "source.shadowed" {
		t.Errorf("List() must return a copy")
	}
}
