// SPDX-License-Identifier: MPL-2.0

package main

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
