package state

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

func TestParamsDigest(t *testing.T) {
	t.Parallel()
	isSecret := func(keys ...string) func(string) bool {
		return func(k string) bool {
			for _, s := range keys {
				if s == k {
					return true
				}
			}
			return false
		}
	}
	tests := []struct {
		name   string
		params map[string]string
		secret func(string) bool
		want   string
	}{
		{name: "nil params", params: nil, want: sum("")},
		{name: "empty params", params: map[string]string{}, secret: isSecret(), want: sum("")},
		{name: "sorted pairs", params: map[string]string{"B": "2", "A": "1"}, want: sum("A=1\nB=2\n")},
		{name: "empty value", params: map[string]string{"A": ""}, want: sum("A=\n")},
		{name: "value is quoted like envfile", params: map[string]string{"A": "x=y\nz"}, want: sum("A=\"x=y\nz\"\n")},
		{name: "quote and backslash escaped", params: map[string]string{"A": `a"b\c`}, want: sum("A=\"a\\\"b\\\\c\"\n")},
		{name: "bare value stays bare", params: map[string]string{"A": "p:443/x,y@z"}, want: sum("A=p:443/x,y@z\n")},
		{
			name:   "secret becomes literal",
			params: map[string]string{"TOKEN": "s3cr3t", "PROXY": "p:443"},
			secret: isSecret("TOKEN"),
			want:   sum("PROXY=p:443\nTOKEN=<secret>\n"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ParamsDigest(tc.params, tc.secret); got != tc.want {
				t.Errorf("ParamsDigest = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParamsDigestInvariants(t *testing.T) {
	t.Parallel()
	secret := func(k string) bool { return k == "TOKEN" }
	base := ParamsDigest(map[string]string{"A": "1", "B": "2", "TOKEN": "one"}, secret)
	tests := []struct {
		name   string
		params map[string]string
		same   bool
	}{
		{name: "key order does not matter", params: map[string]string{"TOKEN": "one", "B": "2", "A": "1"}, same: true},
		{name: "secret value does not matter", params: map[string]string{"A": "1", "B": "2", "TOKEN": "two"}, same: true},
		{name: "empty secret is still a secret", params: map[string]string{"A": "1", "B": "2", "TOKEN": ""}, same: true},
		{name: "non-secret value matters", params: map[string]string{"A": "1", "B": "3", "TOKEN": "one"}},
		{name: "secret presence matters", params: map[string]string{"A": "1", "B": "2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParamsDigest(tc.params, secret)
			if (got == base) != tc.same {
				t.Errorf("ParamsDigest = %s, base %s, want same=%v", got, base, tc.same)
			}
		})
	}
}

// TestParamsDigestNoCollisions: a value cannot forge another key's line, and
// a plain value equal to the marker differs from a real secret.
func TestParamsDigestNoCollisions(t *testing.T) {
	t.Parallel()
	isP := func(k string) bool { return k == "P" }
	tests := []struct {
		name             string
		a, b             map[string]string
		secretA, secretB func(string) bool
	}{
		{name: "newline in value vs two keys", a: map[string]string{"A": "1\nB=2"}, b: map[string]string{"A": "1", "B": "2"}},
		{name: "marker as plain value vs secret", a: map[string]string{"P": "<secret>"}, b: map[string]string{"P": "x"}, secretB: isP},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if ParamsDigest(tc.a, tc.secretA) == ParamsDigest(tc.b, tc.secretB) {
				t.Errorf("digests of %q and %q collide", tc.a, tc.b)
			}
		})
	}
}
