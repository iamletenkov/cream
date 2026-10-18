package state

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// SecretLiteral replaces a secret value in ParamsDigest: only the fact that
// the secret is set enters the digest, no hash of it is stored anywhere.
const SecretLiteral = "<secret>"

// ParamsDigest is "sha256:<hex>" of the sorted "KEY=VALUE\n" lines of the
// install parameters (§10); a key for which secret reports true contributes
// "KEY=<secret>\n". secret may be nil (no secrets).
func ParamsDigest(params map[string]string, secret func(key string) bool) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		v := params[k]
		if secret != nil && secret(k) {
			v = SecretLiteral
		}
		h.Write([]byte(k + "=" + v + "\n"))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
