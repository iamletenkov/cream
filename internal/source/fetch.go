package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
)

// MaxArchiveSize is the download limit of an archive source (§9.2).
const MaxArchiveSize int64 = 256 << 20

// maxRedirects is the number of https→https redirects a download follows.
const maxRedirects = 5

// bodyIdleTimeout cuts a download whose body brings no data for this long
// (69 source.fetch): the overall client timeout alone would wait 30 minutes.
const bodyIdleTimeout = 60 * time.Second

// Fetcher downloads url into w, refusing more than limit bytes. Errors are
// *cerr.Error with exit 69: source.fetch, source.redirect, source.too_large.
type Fetcher interface {
	Fetch(ctx context.Context, url string, w io.Writer, limit int64) error
}

type httpFetcher struct {
	client *http.Client
	idle   time.Duration // body idle timeout; bodyIdleTimeout, shorter in tests
}

// NewHTTPFetcher returns the stdlib fetcher: https:// with the system CAs
// (or the client's transport) and file:///absolute. Redirects are followed
// only https→https and at most 5 times. A body that brings no data for 60
// seconds is cut off (69 source.fetch). client is copied, not modified; nil
// means a client with connection and header timeouts.
func NewHTTPFetcher(client *http.Client) Fetcher {
	var c http.Client
	if client != nil {
		c = *client
	} else {
		c = http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ForceAttemptHTTP2:     true,
		}, Timeout: 30 * time.Minute}
	}
	c.CheckRedirect = checkRedirect
	return &httpFetcher{client: &c, idle: bodyIdleTimeout}
}

// redirectError is returned by CheckRedirect and recognized after Do.
type redirectError struct{ msg string }

func (e *redirectError) Error() string { return e.msg }

func checkRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return &redirectError{msg: fmt.Sprintf("перенаправление на %s: разрешено только https", req.URL.Redacted())}
	}
	if len(via) > maxRedirects {
		return &redirectError{msg: fmt.Sprintf("больше %d перенаправлений", maxRedirects)}
	}
	return nil
}

func (f *httpFetcher) Fetch(ctx context.Context, rawURL string, w io.Writer, limit int64) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return cerr.Wrap(err, cerr.ExitSource, "source.fetch", "загрузка")
	}
	shown := u.Redacted()
	switch u.Scheme {
	case "file":
		if u.Host != "" {
			return cerr.New(cerr.ExitSource, "source.fetch", "загрузка %s: нужен file:///абсолютный/путь", shown)
		}
		return fetchFile(u.Path, w, limit)
	case "https":
	default:
		return cerr.New(cerr.ExitSource, "source.fetch", "загрузка %s: только https:// или file://", shown)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return cerr.Wrap(err, cerr.ExitSource, "source.fetch", "загрузка %s", shown)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		var re *redirectError
		if errors.As(err, &re) {
			return cerr.New(cerr.ExitSource, "source.redirect", "загрузка %s: %s", shown, re.msg)
		}
		return cerr.Wrap(err, cerr.ExitSource, "source.fetch", "загрузка %s", shown)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return cerr.New(cerr.ExitSource, "source.fetch", "загрузка %s: ответ %s", shown, resp.Status)
	}
	if resp.ContentLength > limit {
		return tooLarge(shown, limit)
	}
	body := &idleReader{r: resp.Body, idle: f.idle, timer: time.AfterFunc(f.idle, cancel)}
	defer body.timer.Stop()
	err = copyLimited(w, body, limit, shown)
	if err != nil && body.timedOut() {
		return cerr.New(cerr.ExitSource, "source.fetch", "загрузка %s: нет данных %v", shown, f.idle)
	}
	return err
}

// idleReader cancels the request (via timer) when no Read brings data for
// idle; each Read with data restarts the timer.
type idleReader struct {
	r     io.Reader
	idle  time.Duration
	timer *time.Timer
	fired bool
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		if !r.timer.Stop() {
			r.fired = true
		} else {
			r.timer.Reset(r.idle)
		}
	}
	return n, err
}

// timedOut reports whether the idle timer cancelled the request.
func (r *idleReader) timedOut() bool { return r.fired || !r.timer.Stop() }

func fetchFile(path string, w io.Writer, limit int64) error {
	fd, err := os.Open(path)
	if err != nil {
		return cerr.Wrap(err, cerr.ExitSource, "source.fetch", "чтение %s", path)
	}
	defer func() { _ = fd.Close() }()
	st, err := fd.Stat()
	if err != nil {
		return cerr.Wrap(err, cerr.ExitSource, "source.fetch", "чтение %s", path)
	}
	if !st.Mode().IsRegular() {
		return cerr.New(cerr.ExitSource, "source.fetch", "чтение %s: не обычный файл", path)
	}
	if st.Size() > limit {
		return tooLarge(path, limit)
	}
	return copyLimited(w, fd, limit, path)
}

func copyLimited(w io.Writer, r io.Reader, limit int64, shown string) error {
	n, err := io.Copy(w, io.LimitReader(r, limit+1))
	if err != nil {
		return cerr.Wrap(err, cerr.ExitSource, "source.fetch", "загрузка %s", shown)
	}
	if n > limit {
		return tooLarge(shown, limit)
	}
	return nil
}

func tooLarge(shown string, limit int64) error {
	return cerr.New(cerr.ExitSource, "source.too_large", "загрузка %s: больше %d байт", shown, limit)
}
