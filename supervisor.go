// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

var (
	updateCheckInterval = 6 * time.Hour
	restartPollInterval = time.Minute
)

// ctlTimeout bounds a single soloist ctl call so a hung daemon cannot stall
// the supervisor loop.
const ctlTimeout = 10 * time.Second

type updateResult struct {
	updated bool
	err     error
}

func (a app) runDaemon(ctx context.Context, args []string) error {
	a = a.synchronizedWriters()
	if len(args) != 0 {
		return &commandError{code: 2, err: errors.New("run takes no arguments")}
	}
	p, cfg, err := configuredPaths()
	if err != nil {
		return err
	}
	if err := a.ensureBinary(ctx, p); err != nil {
		return err
	}
	if _, err := a.checkForUpdate(ctx, p); err != nil {
		fmt.Fprintf(a.stderr, "soloistd: update check failed; continuing with installed build: %v\n", err)
	}
	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := a.watchForUpdates(daemonCtx, p)
	restartReady := func(ctx context.Context) bool { return a.playbackIdle(ctx, p, cfg) }
	if !cfg.webSocketEnabled() {
		// Without the WebSocket API ctl cannot see playback; restart right away.
		restartReady = func(context.Context) bool { return true }
	}

	var lastExpiryRefresh time.Time
	for {
		code, err, updated := a.runSoloistOnce(daemonCtx, p.soloistBinary(), cfg.soloistArgs(p), updates, restartReady)
		if ctx.Err() != nil {
			return nil
		}
		if updated {
			fmt.Fprintln(a.stdout, "Restarting Soloist with the new build...")
			continue
		}
		if code != 10 {
			if err == nil {
				return nil
			}
			return &commandError{code: code, err: fmt.Errorf("Soloist exited: %w", err)}
		}
		if !lastExpiryRefresh.IsZero() && time.Since(lastExpiryRefresh) < 5*time.Minute {
			return errors.New("freshly downloaded Soloist build also reported expiry")
		}
		fmt.Fprintln(a.stderr, "Soloist build expired (exit 10); downloading the current build...")
		if err := a.installLatest(ctx, p); err != nil {
			return fmt.Errorf("refresh expired Soloist build: %w", err)
		}
		lastExpiryRefresh = time.Now()
		fmt.Fprintln(a.stdout, "Restarting Soloist with the new build...")
	}
}

func (a app) watchForUpdates(ctx context.Context, p paths) <-chan updateResult {
	results := make(chan updateResult, 1)
	go func() {
		ticker := time.NewTicker(updateCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updated, err := a.checkForUpdate(ctx, p)
				select {
				case results <- updateResult{updated: updated, err: err}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return results
}

// runSoloistOnce runs Soloist until it exits or ctx is canceled. Once an update
// has been installed, it polls restartReady and restarts Soloist only when that
// reports that a restart will not interrupt playback. A build that never
// becomes idle is replaced at the latest when it expires (exit 10).
func (a app) runSoloistOnce(ctx context.Context, binary string, args []string, updates <-chan updateResult, restartReady func(context.Context) bool) (int, error, bool) {
	cmd := exec.Command(binary, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.stdout, a.stderr
	if err := cmd.Start(); err != nil {
		return 1, err, false
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var poll <-chan time.Time
	for {
		select {
		case err := <-done:
			return exitStatus(err), err, false
		case result := <-updates:
			if result.err != nil {
				fmt.Fprintf(a.stderr, "soloistd: update check failed; continuing with installed build: %v\n", result.err)
				continue
			}
			if !result.updated || poll != nil {
				continue
			}
			if restartReady(ctx) {
				return stopSoloist(cmd, done, nil, true)
			}
			fmt.Fprintln(a.stdout, "New Soloist build installed; restarting once playback is paused or idle.")
			poll = time.Tick(restartPollInterval)
		case <-poll:
			if restartReady(ctx) {
				return stopSoloist(cmd, done, nil, true)
			}
		case <-ctx.Done():
			return stopSoloist(cmd, done, ctx.Err(), false)
		}
	}
}

// playbackIdle reports whether restarting Soloist would not interrupt playback,
// according to soloist ctl. When the state cannot be determined, it reports
// false so the restart is deferred to the next poll.
func (a app) playbackIdle(ctx context.Context, p paths, cfg config) bool {
	ctx, cancel := context.WithTimeout(ctx, ctlTimeout)
	defer cancel()
	// After an update this is the new build on disk talking to the old daemon
	// over the WebSocket API.
	cmd := ctlCommand(ctx, p, cfg, "now", "--json")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		// Exit 3 covers every server error. Only a missing login means nothing
		// can be playing; other errors say nothing about playback.
		return exitStatus(err) == 3 && reportsLoggedOut(errOut.String())
	}
	var state struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out.Bytes(), &state); err != nil {
		return false
	}
	return state.Status == "idle" || state.Status == "paused"
}

// reportsLoggedOut matches soloist ctl's error output for a daemon without a
// Spotify session. The exact wording is undocumented, so it matches loosely.
func reportsLoggedOut(stderr string) bool {
	stderr = strings.ToLower(stderr)
	return strings.Contains(stderr, "logged in") ||
		strings.Contains(stderr, "login") ||
		strings.Contains(stderr, "authentication")
}

func stopSoloist(cmd *exec.Cmd, done <-chan error, returnErr error, updated bool) (int, error, bool) {
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
		return 0, returnErr, updated
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return 0, returnErr, updated
	}
}
