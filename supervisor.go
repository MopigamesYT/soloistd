// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

var updateCheckInterval = 6 * time.Hour

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

	var lastExpiryRefresh time.Time
	for {
		code, err, updated := a.runSoloistOnce(daemonCtx, p.soloistBinary(), cfg.soloistArgs(p), updates)
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

func (a app) runSoloistOnce(ctx context.Context, binary string, args []string, updates <-chan updateResult) (int, error, bool) {
	cmd := exec.Command(binary, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.stdout, a.stderr
	if err := cmd.Start(); err != nil {
		return 1, err, false
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	for {
		select {
		case err := <-done:
			return exitStatus(err), err, false
		case result := <-updates:
			if result.err != nil {
				fmt.Fprintf(a.stderr, "soloistd: update check failed; continuing with installed build: %v\n", result.err)
				continue
			}
			if !result.updated {
				continue
			}
			return stopSoloist(cmd, done, nil, true)
		case <-ctx.Done():
			return stopSoloist(cmd, done, ctx.Err(), false)
		}
	}
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
