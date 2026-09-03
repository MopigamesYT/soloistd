// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const serviceName = "soloistd.service"

func (a app) service(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &commandError{code: 2, err: errors.New("usage: soloistd service install|uninstall|start|stop|restart|status|logs")}
	}
	switch args[0] {
	case "install":
		return a.installService(ctx)
	case "uninstall":
		return a.uninstallService(ctx)
	case "start", "stop", "restart":
		return a.systemctl(ctx, args[0], serviceName)
	case "status":
		return a.runExternal(ctx, "systemctl", "--user", "status", "--no-pager", serviceName)
	case "logs":
		return a.runExternal(ctx, "journalctl", "--user", "--unit", serviceName, "--follow")
	default:
		return &commandError{code: 2, err: fmt.Errorf("unknown service action %q", args[0])}
	}
}

func (a app) installService(ctx context.Context) error {
	if !commandExists("systemctl") {
		return errors.New("systemctl is not available; use 'soloistd run' under another process manager")
	}
	p, _, err := configuredPaths()
	if err != nil {
		return err
	}
	executable, err := executablePath()
	if err != nil {
		return err
	}
	unit := serviceUnit(executable)
	if err := ensureParent(p.unitFile, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p.unitFile, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write user service: %w", err)
	}
	if err := a.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err := a.systemctl(ctx, "enable", "--now", serviceName); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Installed and started %s\n", p.unitFile)
	return nil
}

func serviceUnit(executable string) string {
	return `[Unit]
Description=Spotify Soloist daemon
Documentation=https://developer.spotify.com/documentation/soloist
After=network-online.target pipewire.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + quoteSystemdArg(executable) + ` run
Restart=on-failure
RestartSec=60s
UMask=0077
NoNewPrivileges=true

[Install]
WantedBy=default.target
`
}

func (a app) uninstallService(ctx context.Context) error {
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	_ = a.systemctl(ctx, "disable", "--now", serviceName)
	if err := os.Remove(p.unitFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove user service: %w", err)
	}
	if err := a.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "Uninstalled soloistd user service (configuration and playback data were kept).")
	return nil
}

func (a app) systemctl(ctx context.Context, args ...string) error {
	return a.runExternal(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

func (a app) runExternal(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.stdout, a.stderr
	if err := cmd.Run(); err != nil {
		label := strings.Join(append([]string{name}, args...), " ")
		return &commandError{code: exitStatus(err), err: fmt.Errorf("%s: %w", label, err)}
	}
	return nil
}
