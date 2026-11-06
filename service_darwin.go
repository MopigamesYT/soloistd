// SPDX-License-Identifier: MPL-2.0
//go:build darwin

package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const launchAgentLabel = "com.spotify.soloistd"

func (a app) service(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return &commandError{code: 2, err: errors.New("usage: soloistd service install|uninstall|start|stop|restart|status|logs")}
	}
	switch args[0] {
	case "install":
		return a.installService(ctx)
	case "uninstall":
		return a.uninstallService(ctx)
	case "start":
		return a.startService(ctx)
	case "stop":
		return a.launchctl(ctx, "bootout", a.launchService())
	case "restart":
		return a.launchctl(ctx, "kickstart", "-k", a.launchService())
	case "status":
		return a.launchctl(ctx, "print", a.launchService())
	case "logs":
		p, err := resolvePaths()
		if err != nil {
			return err
		}
		return a.runExternal(ctx, "tail", "-F", p.logFile)
	default:
		return &commandError{code: 2, err: fmt.Errorf("unknown service action %q", args[0])}
	}
}

func (a app) installService(ctx context.Context) error {
	if !commandExists("launchctl") {
		return errors.New("launchctl is not available; use 'soloistd run' under another process manager")
	}
	p, _, err := configuredPaths()
	if err != nil {
		return err
	}
	executable, err := executablePath()
	if err != nil {
		return err
	}
	if err := ensureParent(p.serviceFile, 0o755); err != nil {
		return err
	}
	if err := ensureParent(p.logFile, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p.serviceFile, []byte(launchAgent(executable, p.logFile)), 0o644); err != nil {
		return fmt.Errorf("write launch agent: %w", err)
	}
	_ = a.launchctl(ctx, "bootout", a.launchService())
	if err := a.startService(ctx); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Installed and started %s\n", p.serviceFile)
	return nil
}

func (a app) uninstallService(ctx context.Context) error {
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	_ = a.launchctl(ctx, "bootout", a.launchService())
	if err := os.Remove(p.serviceFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove launch agent: %w", err)
	}
	fmt.Fprintln(a.stdout, "Uninstalled soloistd user service (configuration and playback data were kept).")
	return nil
}

func (a app) startService(ctx context.Context) error {
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	return a.launchctl(ctx, "bootstrap", a.launchDomain(), p.serviceFile)
}

func (a app) launchDomain() string  { return "gui/" + strconv.Itoa(os.Getuid()) }
func (a app) launchService() string { return a.launchDomain() + "/" + launchAgentLabel }
func (a app) launchctl(ctx context.Context, args ...string) error {
	return a.runExternal(ctx, "launchctl", args...)
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

func launchAgent(executable, logFile string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + launchAgentLabel + `</string>
<key>ProgramArguments</key><array><string>` + xmlEscape(executable) + `</string><string>run</string></array>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>ThrottleInterval</key><integer>60</integer>
<key>ProcessType</key><string>Background</string>
<key>Umask</key><integer>63</integer>
<key>StandardOutPath</key><string>` + xmlEscape(logFile) + `</string>
<key>StandardErrorPath</key><string>` + xmlEscape(logFile) + `</string>
</dict></plist>
`
}

func xmlEscape(value string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}
