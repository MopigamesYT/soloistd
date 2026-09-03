// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"
)

const version = "0.2.0"

type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(data)
}

func (a app) synchronizedWriters() app {
	if _, ok := a.stdout.(*lockedWriter); !ok {
		a.stdout = &lockedWriter{w: a.stdout}
	}
	if _, ok := a.stderr.(*lockedWriter); !ok {
		a.stderr = &lockedWriter{w: a.stderr}
	}
	return a
}

func main() {
	a := app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(a.run(ctx, os.Args[1:]))
}

func (a app) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		a.usage()
		return 2
	}

	var err error
	switch args[0] {
	case "setup":
		err = a.setup(ctx, args[1:])
	case "run":
		err = a.runDaemon(ctx, args[1:])
	case "pair", "login":
		err = a.pair(ctx, args[1:])
	case "update":
		err = a.update(ctx, args[1:])
	case "ctl":
		err = a.ctl(ctx, args[1:])
	case "service":
		err = a.service(ctx, args[1:])
	case "version", "--version", "-V":
		fmt.Fprintf(a.stdout, "soloistd %s\n", version)
		return 0
	case "help", "--help", "-h":
		a.usage()
		return 0
	default:
		fmt.Fprintf(a.stderr, "soloistd: unknown command %q\n\n", args[0])
		a.usage()
		return 2
	}

	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		return 0
	}
	var exitErr *commandError
	if errors.As(err, &exitErr) {
		if exitErr.err != nil {
			fmt.Fprintf(a.stderr, "soloistd: %v\n", exitErr.err)
		}
		return exitErr.code
	}
	fmt.Fprintf(a.stderr, "soloistd: %v\n", err)
	return 1
}

func (a app) usage() {
	fmt.Fprint(a.stdout, `soloistd manages Spotify Soloist as a user service.

Usage:
  soloistd setup [--device-name NAME] [--api-key KEY]
  soloistd run
  soloistd pair
  soloistd update
  soloistd ctl COMMAND [ARGS...]
  soloistd service install|uninstall|start|stop|restart|status|logs
  soloistd version

Quick start:
  soloistd setup
  soloistd service install

After the service starts, select its device name in the Spotify app. The API
key can also be supplied to setup through the SOLOIST_API_KEY environment
variable, keeping it out of shell history.
`)
}

type commandError struct {
	code int
	err  error
}

func (e *commandError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("command exited with status %d", e.code)
	}
	return e.err.Error()
}
func (e *commandError) Unwrap() error { return e.err }

func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if status, ok := ee.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return 128 + int(status.Signal())
			}
			return status.ExitStatus()
		}
	}
	return 1
}

func executablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
		path = resolved
	}
	return path, nil
}

func quoteSystemdArg(value string) string {
	return strconv.Quote(value)
}

func isTerminalReader(r io.Reader) (*os.File, bool) {
	f, ok := r.(*os.File)
	return f, ok && term.IsTerminal(int(f.Fd()))
}

func nonEmpty(value, name string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", name)
	}
	return value, nil
}
