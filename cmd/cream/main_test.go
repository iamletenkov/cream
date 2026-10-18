package main

import (
	"bytes"
	"strings"
	"testing"
)

// main — только umask и cli.Run; здесь проверяется, что run собирает App и
// отдаёт код выхода диспетчера. Сам диспетчер покрыт internal/cli.
func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "version", args: []string{"version"}, wantCode: 0, wantStdout: "cream dev\n"},
		{name: "version json", args: []string{"version", "--json"}, wantCode: 0, wantStdout: `"contract": "cream/v1"`},
		{name: "no args", args: nil, wantCode: 64, wantStderr: "cream <команда>"},
		{name: "unknown command", args: []string{"frobnicate"}, wantCode: 64, wantStderr: `"frobnicate"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			env := func(string) string { return "" }
			code := run(tc.args, strings.NewReader(""), &stdout, &stderr, env)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tc.wantStdout)
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}
