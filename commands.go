// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func (a app) setup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	deviceName := fs.String("device-name", "", "Spotify Connect device name")
	apiKey := fs.String("api-key", "", "Spotify Soloist API key (prefer SOLOIST_API_KEY)")
	if err := fs.Parse(args); err != nil {
		return &commandError{code: 2, err: err}
	}
	if fs.NArg() != 0 {
		return &commandError{code: 2, err: fmt.Errorf("setup takes no positional arguments")}
	}

	reader := bufio.NewReader(a.stdin)
	if strings.TrimSpace(*deviceName) == "" {
		hostname, _ := os.Hostname()
		defaultName := defaultDeviceName(hostname)
		fmt.Fprintf(a.stdout, "Device name [%s]: ", defaultName)
		line, err := readLineContext(ctx, reader)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		*deviceName = strings.TrimSpace(line)
		if *deviceName == "" {
			*deviceName = defaultName
		}
	}
	if *apiKey == "" {
		*apiKey = os.Getenv("SOLOIST_API_KEY")
	}
	if *apiKey == "" {
		fmt.Fprint(a.stdout, "Soloist API key: ")
		if input, ok := isTerminalReader(a.stdin); ok {
			secret, err := readPasswordContext(ctx, input)
			fmt.Fprintln(a.stdout)
			if err != nil {
				return err
			}
			*apiKey = string(secret)
		} else {
			line, err := readLineContext(ctx, reader)
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			*apiKey = strings.TrimSpace(line)
		}
	}
	var err error
	*deviceName, err = nonEmpty(*deviceName, "device name")
	if err != nil {
		return err
	}
	*apiKey, err = nonEmpty(*apiKey, "API key")
	if err != nil {
		return err
	}

	p, err := resolvePaths()
	if err != nil {
		return err
	}
	cfg := config{DeviceName: *deviceName, APIKey: *apiKey}
	if old, loadErr := loadConfig(p); loadErr == nil {
		cfg.DataDir = old.DataDir
		cfg.CacheDir = old.CacheDir
		cfg.CacheSizeMB = old.CacheSizeMB
		cfg.PipewireDevice = old.PipewireDevice
		cfg.InitialVolume = old.InitialVolume
		cfg.WebSocket = old.WebSocket
	}
	if err := saveConfig(p, cfg); err != nil {
		return fmt.Errorf("save configuration: %w", err)
	}
	fmt.Fprintf(a.stdout, "Saved private configuration to %s\n", p.configFile)
	if err := a.installLatest(ctx, p); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "Ready. Run 'soloistd service install', then select the device in Spotify.")
	return nil
}

type lineResult struct {
	line string
	err  error
}

func readLineContext(ctx context.Context, reader *bufio.Reader) (string, error) {
	done := make(chan lineResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		done <- lineResult{line: line, err: err}
	}()
	select {
	case result := <-done:
		return result.line, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func readPasswordContext(ctx context.Context, input *os.File) ([]byte, error) {
	fd := int(input.Fd())
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	noEcho := *state
	noEcho.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &noEcho); err != nil {
		return nil, err
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, state)

	line, err := readLineContext(ctx, bufio.NewReader(input))
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(line, "\r\n")), nil
}

func defaultDeviceName(hostname string) string {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "host"
	}
	return "soloistd@" + hostname
}

func (a app) update(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return &commandError{code: 2, err: errors.New("update takes no arguments")}
	}
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	return a.installLatest(ctx, p)
}

func (a app) pair(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return &commandError{code: 2, err: errors.New("pair takes no arguments")}
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
	fmt.Fprintln(a.stdout, "Pairing: select this device in the Spotify app. Soloist exits when pairing finishes.")
	for attempt := 0; attempt < 2; attempt++ {
		cmd := exec.CommandContext(ctx, p.soloistBinary(), append(cfg.soloistArgs(p), "--pair")...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.stdout, a.stderr
		err := cmd.Run()
		if err == nil {
			return nil
		}
		code := exitStatus(err)
		if code != 10 || attempt == 1 {
			return &commandError{code: code, err: fmt.Errorf("Soloist pairing failed: %w", err)}
		}
		fmt.Fprintln(a.stderr, "Soloist build expired (exit 10); downloading the current build...")
		if err := a.installLatest(ctx, p); err != nil {
			return fmt.Errorf("refresh expired Soloist build: %w", err)
		}
	}
	return errors.New("unreachable")
}

func (a app) ctl(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return &commandError{code: 2, err: errors.New("ctl requires a Soloist control command")}
	}
	p, cfg, err := configuredPaths()
	if err != nil {
		return err
	}
	if err := a.ensureBinary(ctx, p); err != nil {
		return err
	}
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = p.soloistData
	}
	ctlArgs := append([]string{"ctl"}, args...)
	ctlArgs = append(ctlArgs, "--data-dir", dataDir)
	cmd := exec.CommandContext(ctx, p.soloistBinary(), ctlArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.stdout, a.stderr
	if err := cmd.Run(); err != nil {
		return &commandError{code: exitStatus(err), err: nil}
	}
	return nil
}

func configuredPaths() (paths, config, error) {
	p, err := resolvePaths()
	if err != nil {
		return paths{}, config{}, err
	}
	cfg, err := loadConfig(p)
	return p, cfg, err
}

func (a app) ensureBinary(ctx context.Context, p paths) error {
	info, err := os.Stat(p.soloistBinary())
	if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return a.installLatest(ctx, p)
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func ensureParent(path string, mode os.FileMode) error {
	return os.MkdirAll(filepath.Dir(path), mode)
}
