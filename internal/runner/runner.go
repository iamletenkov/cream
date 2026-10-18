// Package runner starts extension hooks and actions (spec §6.3, §7.2).
//
// A hook is executed directly, without a shell: absolute path, argv as a
// list, a clean environment built by BuildEnv, its own process group. On a
// timeout the whole group gets SIGTERM and, after a grace period, SIGKILL, so
// that grandchildren (a "sleep" started by a hook script) die with it.
// Output can be passed through a streaming secret filter. The Runner
// interface lets the engine replace process start in tests; Exec is the real
// implementation and holds no global state.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"time"
)

// DefaultGrace is how long a timed-out process group has between SIGTERM and
// SIGKILL when Exec.Grace is zero.
const DefaultGrace = 10 * time.Second

// ErrRelativePath is returned for a Spec.Path that is not absolute: cream
// never looks executables up in PATH.
var ErrRelativePath = errors.New("путь к исполняемому файлу должен быть абсолютным")

// Spec describes one process start.
//
//   - Path is absolute; Args do not include argv[0] (it is Path).
//   - Env is the whole environment; nil means empty, never cream's own.
//   - Stdin nil is /dev/null; Stdout/Stderr nil discard the stream.
//   - Timeout 0 means no timeout (spec §6.1: install has none by default).
//   - ExtraFiles[i] becomes descriptor FirstExtraFD+i in the child.
//   - Secrets, when non-empty, are masked with "***" in both Stdout and
//     Stderr. A caller that wants one stream unfiltered (action stdout, §7.2)
//     wraps the other itself with NewSecretFilter and leaves Secrets empty.
type Spec struct {
	Path       string
	Args       []string
	Dir        string
	Env        []string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Timeout    time.Duration
	ExtraFiles []*os.File
	Secrets    []string
}

// Result is how the process ended. ExitCode is the exit status, or 128+N when
// the process was killed by signal N (as a shell reports it); TimedOut means
// Spec.Timeout expired and the group was killed.
type Result struct {
	ExitCode int
	TimedOut bool
	Duration time.Duration
}

// Runner starts a process and waits for it. An error means the process could
// not be started or its output could not be delivered, or ctx was canceled;
// a non-zero exit code alone is not an error.
type Runner interface {
	Run(ctx context.Context, s Spec) (Result, error)
}

// Func adapts a function to Runner (fakes in engine tests).
type Func func(ctx context.Context, s Spec) (Result, error)

// Run calls fn.
func (fn Func) Run(ctx context.Context, s Spec) (Result, error) { return fn(ctx, s) }

// Exec runs processes with os/exec. Grace is the SIGTERM→SIGKILL interval on
// timeout or cancellation (zero — DefaultGrace); it also bounds how long Run
// waits for output pipes held open by descendants after the process exits.
type Exec struct {
	Grace time.Duration
}

// Run starts s.Path and waits for it.
func (e Exec) Run(ctx context.Context, s Spec) (Result, error) {
	if !filepath.IsAbs(s.Path) {
		return Result{ExitCode: -1}, fmt.Errorf("%s: %w", s.Path, ErrRelativePath)
	}
	grace := e.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}
	runCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	stdout, stderr, closeFilters := outputs(s)

	// exec.CommandContext only binds the context: Path is absolute, so there is
	// no PATH lookup, and every other field is set here.
	cmd := exec.CommandContext(runCtx, s.Path)
	cmd.Args = append([]string{s.Path}, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	if cmd.Env == nil {
		cmd.Env = []string{} // nil would hand the child cream's own environment
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s.Stdin, stdout, stderr
	cmd.ExtraFiles = s.ExtraFiles
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = grace
	var killTimer *time.Timer
	cmd.Cancel = func() error {
		pgid := cmd.Process.Pid
		killTimer = time.AfterFunc(grace, func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		_ = closeFilters()
		return Result{ExitCode: -1}, fmt.Errorf("запуск %s: %w", s.Path, err)
	}
	waitErr := cmd.Wait()
	res := Result{Duration: time.Since(start), ExitCode: exitCode(cmd.ProcessState)}
	if killTimer != nil {
		killTimer.Stop()
		// Leftovers that ignored SIGTERM, if the leader died early.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	flushErr := closeFilters()

	if ctx.Err() != nil {
		return res, fmt.Errorf("%s: %w", s.Path, ctx.Err())
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		return res, nil
	}
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil, errors.As(waitErr, &exitErr), errors.Is(waitErr, exec.ErrWaitDelay):
		// A non-zero exit is a result, not an error; ErrWaitDelay means the
		// process exited but a descendant kept its output open — not ours.
	default:
		return res, fmt.Errorf("%s: %w", s.Path, waitErr)
	}
	if flushErr != nil {
		return res, fmt.Errorf("%s: вывод: %w", s.Path, flushErr)
	}
	return res, nil
}

// outputs wraps Stdout and Stderr in secret filters. The same writer for both
// gets one filter, so os/exec still serializes writes to it.
func outputs(s Spec) (stdout, stderr io.Writer, closeAll func() error) {
	stdout, stderr = s.Stdout, s.Stderr
	if !hasSecrets(s.Secrets) {
		return stdout, stderr, func() error { return nil }
	}
	var filters []io.WriteCloser
	wrap := func(w io.Writer) io.Writer {
		if w == nil {
			return nil
		}
		f := NewSecretFilter(w, s.Secrets)
		filters = append(filters, f)
		return f
	}
	same := sameWriter(stdout, stderr)
	stdout = wrap(stdout)
	if same {
		stderr = stdout
	} else {
		stderr = wrap(stderr)
	}
	return stdout, stderr, func() error {
		var errs []error
		for _, f := range filters {
			errs = append(errs, f.Close())
		}
		return errors.Join(errs...)
	}
}

func hasSecrets(secrets []string) bool {
	for _, s := range secrets {
		if s != "" {
			return true
		}
	}
	return false
}

// sameWriter reports whether a and b are the same comparable writer; the
// reflect check avoids a panic on == for non-comparable dynamic types.
func sameWriter(a, b io.Writer) bool {
	if a == nil || b == nil || !reflect.TypeOf(a).Comparable() || !reflect.TypeOf(b).Comparable() {
		return false
	}
	return a == b
}

// exitCode returns the exit status, 128+signal for a killed process, -1 when
// unknown.
func exitCode(ps *os.ProcessState) int {
	if ps == nil {
		return -1
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ps.ExitCode()
}
