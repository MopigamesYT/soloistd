// SPDX-License-Identifier: MPL-2.0
//go:build darwin

package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestLaunchAgentEscapesPathsAndContainsNoSecret(t *testing.T) {
	agent := launchAgent(`/Applications/Soloist & Tools/soloistd`, `/Users/mopi/Library/Logs/soloistd/soloistd.log`)
	if !strings.Contains(agent, `<string>/Applications/Soloist &amp; Tools/soloistd</string><string>run</string>`) {
		t.Fatalf("launch agent has incorrect ProgramArguments:\n%s", agent)
	}
	if strings.Contains(strings.ToLower(agent), "api-key") {
		t.Fatal("launch agent must not contain the API key option")
	}
	var document struct{}
	if err := xml.Unmarshal([]byte(agent), &document); err != nil {
		t.Fatalf("launch agent is not valid XML: %v", err)
	}
}
