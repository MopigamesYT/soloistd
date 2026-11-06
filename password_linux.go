// SPDX-License-Identifier: MPL-2.0
//go:build linux

package main

import (
	"bufio"
	"context"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

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
