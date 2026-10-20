// Command genman writes cream's manual pages (cream.1.gz,
// cream-extension.yaml.5.gz) into a directory. The pages are rendered by
// internal/cli from the same command table as --help; the date comes from
// SOURCE_DATE_EPOCH and gzip carries no name and no time, so the output is
// reproducible. Used by make deb-pkg: go run ./tools/genman -out DIR -version V.
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/iamletenkov/cream/internal/cli"
)

func main() {
	out := flag.String("out", "", "output directory")
	version := flag.String("version", "dev", "version for the page footer")
	plain := flag.Bool("plain", false, "also write uncompressed pages")
	flag.Parse()
	if err := run(*out, *version, *plain); err != nil {
		fmt.Fprintln(os.Stderr, "genman:", err)
		os.Exit(1)
	}
}

func run(out, version string, plain bool) error {
	if out == "" {
		return fmt.Errorf("-out is required")
	}
	epoch, err := strconv.ParseInt(os.Getenv("SOURCE_DATE_EPOCH"), 10, 64)
	if err != nil {
		return fmt.Errorf("SOURCE_DATE_EPOCH: %w", err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	pages := cli.App{}.ManPages(version, time.Unix(epoch, 0))
	names := make([]string, 0, len(pages))
	for n := range pages {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if plain {
			if err := os.WriteFile(filepath.Join(out, n), []byte(pages[n]), 0o644); err != nil {
				return err
			}
		}
		if err := writeGzip(filepath.Join(out, n+".gz"), pages[n]); err != nil {
			return err
		}
		fmt.Println(filepath.Join(out, n+".gz"))
	}
	return nil
}

// writeGzip writes s gzipped at the best compression, with no name and no
// modification time in the header.
func writeGzip(path, s string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	zw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := zw.Write([]byte(s)); err != nil {
		_ = f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
