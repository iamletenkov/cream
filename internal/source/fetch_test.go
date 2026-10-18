package source

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
)

func TestHTTPFetcher(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("x", 1000)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(plain.Close)
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ok":
			_, _ = w.Write([]byte(body))
		case r.URL.Path == "/missing":
			http.NotFound(w, r)
		case r.URL.Path == "/to-http":
			http.Redirect(w, r, plain.URL+"/ok", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/hop/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop/"))
			if n == 0 {
				_, _ = w.Write([]byte(body))
				return
			}
			http.Redirect(w, r, fmt.Sprintf("%s/hop/%d", srv.URL, n-1), http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	file := filepath.Join(t.TempDir(), "c.tar.gz")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		url      string
		limit    int64
		wantExit int
		wantCode string
	}{
		{name: "https", url: srv.URL + "/ok", limit: MaxArchiveSize},
		{name: "five redirects https→https", url: srv.URL + "/hop/5", limit: MaxArchiveSize},
		{name: "six redirects", url: srv.URL + "/hop/6", limit: MaxArchiveSize,
			wantExit: cerr.ExitSource, wantCode: "source.redirect"},
		{name: "redirect https→http", url: srv.URL + "/to-http", limit: MaxArchiveSize,
			wantExit: cerr.ExitSource, wantCode: "source.redirect"},
		{name: "http is refused", url: plain.URL + "/ok", limit: MaxArchiveSize,
			wantExit: cerr.ExitSource, wantCode: "source.fetch"},
		{name: "404", url: srv.URL + "/missing", limit: MaxArchiveSize,
			wantExit: cerr.ExitSource, wantCode: "source.fetch"},
		{name: "limit exactly the size", url: srv.URL + "/ok", limit: int64(len(body))},
		{name: "over the limit", url: srv.URL + "/ok", limit: int64(len(body)) - 1,
			wantExit: cerr.ExitSource, wantCode: "source.too_large"},
		{name: "file://", url: "file://" + file, limit: MaxArchiveSize},
		{name: "file:// over the limit", url: "file://" + file, limit: 10,
			wantExit: cerr.ExitSource, wantCode: "source.too_large"},
		{name: "file:// missing", url: "file://" + file + ".none", limit: MaxArchiveSize,
			wantExit: cerr.ExitSource, wantCode: "source.fetch"},
		{name: "file:// directory", url: "file://" + filepath.Dir(file), limit: MaxArchiveSize,
			wantExit: cerr.ExitSource, wantCode: "source.fetch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := NewHTTPFetcher(srv.Client())
			var buf bytes.Buffer
			err := f.Fetch(context.Background(), tc.url, &buf, tc.limit)
			if tc.wantExit != 0 {
				wantCerr(t, err, tc.wantExit, tc.wantCode)
				return
			}
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if buf.String() != body {
				t.Errorf("получено %d байт, ждали %d", buf.Len(), len(body))
			}
		})
	}
}

func TestHTTPFetcherDoesNotMutateClient(t *testing.T) {
	t.Parallel()
	c := &http.Client{}
	_ = NewHTTPFetcher(c)
	if c.CheckRedirect != nil {
		t.Error("NewHTTPFetcher изменил переданный клиент")
	}
	if NewHTTPFetcher(nil) == nil {
		t.Error("NewHTTPFetcher(nil) = nil")
	}
}

// A server that sends part of the body and then goes silent is cut off after
// the idle timeout: 69 source.fetch, not a hang until the overall timeout.
func TestHTTPFetcherIdleTimeout(t *testing.T) {
	t.Parallel()
	stop := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select {
		case <-stop:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })
	f, ok := NewHTTPFetcher(srv.Client()).(*httpFetcher)
	if !ok {
		t.Fatal("не *httpFetcher")
	}
	f.idle = 200 * time.Millisecond
	start := time.Now()
	var buf bytes.Buffer
	err := f.Fetch(context.Background(), srv.URL+"/slow", &buf, MaxArchiveSize)
	wantCerr(t, err, cerr.ExitSource, "source.fetch")
	if el := time.Since(start); el > 10*time.Second {
		t.Errorf("обрыв через %v", el)
	}
	if !strings.Contains(err.Error(), "нет данных") {
		t.Errorf("ошибка %v: ждали упоминание простоя", err)
	}
	if buf.String() != "partial" {
		t.Errorf("получено %q", buf.String())
	}
}
