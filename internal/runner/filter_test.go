package runner

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestSecretFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		secrets []string
		chunks  []string
		want    string
	}{
		{"без секретов", nil, []string{"hello ", "world\n"}, "hello world\n"},
		{"секрет целиком", []string{"s3cr3t"}, []string{"pass=s3cr3t\n"}, "pass=***\n"},
		{"дважды в строке", []string{"ab"}, []string{"ab-ab\n"}, "***-***\n"},
		{"стык двух Write", []string{"s3cr3t"}, []string{"pass=s3c", "r3t\n"}, "pass=***\n"},
		{"по байту", []string{"s3cr3t"}, splitBytes("x s3cr3t y\n"), "x *** y\n"},
		{"стык через три Write", []string{"abcdef"}, []string{"ab", "cd", "ef!"}, "***!"},
		{"перекрывающиеся: длинный раньше", []string{"abc", "abcdef"}, []string{"abcdef abc\n"}, "*** ***\n"},
		{"перекрывающиеся на стыке", []string{"abc", "abcdef"}, []string{"abc", "def\n"}, "***\n"},
		{"короткий внутри длинного", []string{"cd", "abcdef"}, []string{"abcdef cd"}, "*** ***"},
		{"пустой секрет игнорируется", []string{"", "x1"}, []string{"a x1 b"}, "a *** b"},
		{"незавершённый префикс в хвосте", []string{"secret"}, []string{"sec", "ond"}, "second"},
		{"Close сбрасывает хвост-префикс", []string{"secret"}, []string{"data secr"}, "data secr"},
		{"многострочный секрет", []string{"line1\nline2"}, []string{"k=line1\n", "line2\nend\n"}, "k=***\nend\n"},
		{"повторяющийся префикс", []string{"aab"}, []string{"aaab"}, "a***"},
		// Маскируется объединение всех вхождений всех секретов: ни один байт
		// ни одного из перекрывающихся секретов не уходит наружу.
		{"перекрытие abcdef/defxyz", []string{"abcdef", "defxyz"}, []string{"abcdefxyz"}, "***"},
		{"перекрытие в обратном порядке", []string{"defxyz", "abcdef"}, []string{"<abcdefxyz>"}, "<***>"},
		{"перекрытие разной длины", []string{"abc", "bcdefg"}, []string{"abcdefg!"}, "***!"},
		{"вложенный bc в abcd", []string{"bc", "abcd"}, []string{"abcd"}, "***"},
		{"вложенный bc без внешнего", []string{"bc"}, []string{"abcd"}, "a***d"},
		{"перекрытие на стыке двух Write", []string{"abcdef", "defxyz"}, []string{"abcd", "efxyz"}, "***"},
		{"перекрытие на стыке, хвост до Close", []string{"abcdef", "defxyz"}, []string{"abcdefx", "y"}, "***xy"},
		{"смежные вхождения сливаются", []string{"ab"}, []string{"a", "bab"}, "***"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			f := NewSecretFilter(&out, tc.secrets)
			for _, c := range tc.chunks {
				n, err := f.Write([]byte(c))
				if err != nil || n != len(c) {
					t.Fatalf("Write(%q) = %d, %v", c, n, err)
				}
			}
			if err := f.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("вывод %q, ожидали %q", got, tc.want)
			}
		})
	}
}

// Пока данные не могут оказаться началом секрета, фильтр их не задерживает:
// прогресс хука виден сразу, без ожидания конца строки.
func TestSecretFilterDoesNotHoldSafeOutput(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	f := NewSecretFilter(&out, []string{"secret"})
	_, _ = f.Write([]byte("progress ...."))
	if got := out.String(); got != "progress ...." {
		t.Fatalf("до Close выдано %q", got)
	}
	_, _ = f.Write([]byte(" se"))
	if got := out.String(); got != "progress .... " {
		t.Fatalf("возможное начало секрета должно ждать: %q", got)
	}
}

func TestSecretFilterWriteAfterClose(t *testing.T) {
	t.Parallel()
	f := NewSecretFilter(&bytes.Buffer{}, []string{"x"})
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("a")); err == nil {
		t.Fatal("Write после Close должен вернуть ошибку")
	}
	if err := f.Close(); err != nil {
		t.Fatalf("повторный Close: %v", err)
	}
}

type failWriter struct{}

var errBroken = errors.New("сломано")

func (failWriter) Write([]byte) (int, error) { return 0, errBroken }

func TestSecretFilterPropagatesWriteError(t *testing.T) {
	t.Parallel()
	f := NewSecretFilter(failWriter{}, []string{"x"})
	if _, err := f.Write([]byte("abc")); !errors.Is(err, errBroken) {
		t.Fatalf("ошибка %v, ожидали errBroken", err)
	}
}

func TestSecretFilterLargeInput(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	f := NewSecretFilter(&out, []string{"TOPSECRET"})
	in := strings.Repeat("noise TOPSECRET ", 10000)
	for i := 0; i < len(in); i += 4093 {
		_, _ = f.Write([]byte(in[i:min(i+4093, len(in))]))
	}
	_ = f.Close()
	if strings.Contains(out.String(), "TOPSECRET") {
		t.Fatal("секрет просочился")
	}
	if got, want := out.String(), strings.Repeat("noise *** ", 10000); got != want {
		t.Fatalf("длина %d, ожидали %d", len(got), len(want))
	}
}

func splitBytes(s string) []string {
	out := make([]string, len(s))
	for i := range s {
		out[i] = s[i : i+1]
	}
	return out
}
