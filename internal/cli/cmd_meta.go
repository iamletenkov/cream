package cli

import (
	"io"

	"github.com/iamletenkov/cream/internal/version"
)

// metaCommands are commands about cream itself.
func (App) metaCommands() []Command {
	return []Command{
		{Name: "version", Usage: "version [--json]  — версия cream и контракт", Run: runVersion},
	}
}

// versionResult is the result of `cream version` (golden: testdata/golden/version.json).
type versionResult struct {
	Version  string `json:"version"`
	Contract string `json:"contract"`
}

func (v versionResult) WriteText(w io.Writer) error {
	_, err := io.WriteString(w, version.Line(v.Version)+"\n")
	return err
}

func runVersion(ctx *Context, args []string) (any, error) {
	if len(args) > 0 {
		return nil, ctx.Usagef("version: лишние аргументы %q", args)
	}
	return versionResult{Version: creamVersion(), Contract: Contract}, nil
}
