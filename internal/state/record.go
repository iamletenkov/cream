// Package state is the registry of installed extensions and the operation
// journal (spec §10).
//
// The registry is one file per extension, /var/lib/cream/installed/<ext>.json,
// written atomically (fsx.WriteAtomic) by the holder of lock/global. The
// journal /var/lib/cream/history.jsonl is append-only and never carries
// parameters. Locking is the caller's job (package lock).
package state

import "time"

// SchemaV1 is the registry schema written by this cream.
const SchemaV1 = "cream-state/v1"

// schemaPrefix and schemaMajor: cream reads records of the same major schema
// and refuses newer ones with 74 state.newer.
const (
	schemaPrefix = "cream-state/v"
	schemaMajor  = 1
)

// Record statuses.
const (
	StatusInstalled     = "installed"
	StatusFailed        = "failed"
	StatusFailedUpgrade = "failed-upgrade"
	StatusSkipped       = "skipped"
)

// SourceRef is where the installed tree came from.
type SourceRef struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Origin         string `json:"origin"`
	Resolved       string `json:"resolved"`
	CatalogVersion string `json:"catalog_version"`
}

// FileRec is one file laid out by "files", with the digest written.
type FileRec struct {
	Dst    string `json:"dst"`
	SHA256 string `json:"sha256"`
}

// ConfigureRec is the enabled configure stage.
type ConfigureRec struct {
	Stage  string `json:"stage"`
	Target string `json:"target"`
}

// Record is /var/lib/cream/installed/<ext>.json.
type Record struct {
	Schema         string        `json:"schema"`
	Name           string        `json:"name"`
	Version        string        `json:"version"`
	Kind           string        `json:"kind"`
	Status         string        `json:"status"`
	Source         SourceRef     `json:"source"`
	ExtDigest      string        `json:"ext_digest"`
	ParamsDigest   string        `json:"params_digest"`
	Phase          string        `json:"phase"`
	InstalledAt    time.Time     `json:"installed_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	CreamVersion   string        `json:"cream_version"`
	Files          []FileRec     `json:"files"`
	Bin            []string      `json:"bin"`
	Configure      *ConfigureRec `json:"configure"`
	RebootRequired bool          `json:"reboot_required"`
	// PrevVersion is the version an unfinished upgrade started from (status failed-upgrade).
	PrevVersion string `json:"prev_version,omitempty"`
	// Held is `cream hold`: upgrade --all skips the extension, an explicit
	// upgrade or install of another version is refused (68 hold.held).
	Held bool `json:"held,omitempty"`
	// SecretsRev counts writes of secret keys (cream set): params_digest
	// carries only the names of secrets, so the configure mark compares this
	// counter to see a rotated secret (spec bisquite-bundled-cream §10.7).
	// Absent (records of cream 1.1) is 0.
	SecretsRev int `json:"secrets_rev,omitempty"`
}
