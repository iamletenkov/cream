// Command cream is the extension runner. For now it only reports its version;
// the domain commands come with the spec (docs/specs/).
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/iamletenkov/cream/internal/version"
)

const usage = `usage: cream <command>

commands:
  version   print the cream version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command and returns the process exit code:
// 0 on success, 1 if stdout cannot be written, 2 on a usage error.
// Write errors to stderr are ignored: there is nowhere left to report them.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		return writeOut(stdout, version.Line(version.Version)+"\n")
	case "help", "-h", "--help":
		return writeOut(stdout, usage)
	default:
		_, _ = fmt.Fprintf(stderr, "cream: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// writeOut writes s to stdout and maps a write failure (closed pipe, full disk)
// to exit code 1.
func writeOut(stdout io.Writer, s string) int {
	if _, err := io.WriteString(stdout, s); err != nil {
		return 1
	}
	return 0
}
