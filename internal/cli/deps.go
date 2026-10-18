package cli

// The only external dependency of the module (spec §11.1) is pinned in go.mod
// before its first real user; Task 4 (internal/manifest) imports it and
// deletes this file.
import _ "go.yaml.in/yaml/v3" // pins go.yaml.in/yaml/v3 in go.mod/go.sum
