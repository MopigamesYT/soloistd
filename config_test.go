// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveAndLoadConfigSecurely(t *testing.T) {
	root := t.TempDir()
	p := paths{configFile: filepath.Join(root, "config", "soloistd", "config.json")}
	volume := 37
	want := config{DeviceName: "Mopi's PC", APIKey: "very-secret", InitialVolume: &volume}

	if err := saveConfig(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded config = %#v, want %#v", got, want)
	}
	info, err := os.Stat(p.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(p.configFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config directory mode = %o, want 700", got)
	}
}

func TestSoloistArgs(t *testing.T) {
	cacheSize := 500
	volume := 42
	cfg := config{
		DeviceName:     "Bedroom",
		APIKey:         "secret",
		CacheSizeMB:    &cacheSize,
		PipewireDevice: "sink-1",
		InitialVolume:  &volume,
		WebSocket:      "127.0.0.1:0",
	}
	p := paths{soloistData: "/data/soloist", soloistCache: "/cache/soloist"}
	want := []string{
		"--device-name", "Bedroom", "--api-key", "secret",
		"--data-dir", "/data/soloist", "--cache-dir", "/cache/soloist",
		"--cache-size", "500", "--pipewire-device", "sink-1",
		"--initial-volume", "42", "--ws", "127.0.0.1:0",
	}
	if got := cfg.soloistArgs(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("soloistArgs() = %#v, want %#v", got, want)
	}
}

func TestValidateConfigRejectsInvalidAudioValues(t *testing.T) {
	tooSmall := 99
	if err := validateConfig(config{DeviceName: "x", APIKey: "y", CacheSizeMB: &tooSmall}); err == nil {
		t.Fatal("expected invalid cache size to fail")
	}
	tooLoud := 101
	if err := validateConfig(config{DeviceName: "x", APIKey: "y", InitialVolume: &tooLoud}); err == nil {
		t.Fatal("expected invalid volume to fail")
	}
}

func TestDefaultDeviceName(t *testing.T) {
	if got := defaultDeviceName("framework13"); got != "soloistd@framework13" {
		t.Fatalf("defaultDeviceName() = %q", got)
	}
	if got := defaultDeviceName("  "); got != "soloistd@host" {
		t.Fatalf("empty defaultDeviceName() = %q", got)
	}
}

func TestPromptReadStopsWhenContextIsCanceled(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := readLineContext(ctx, bufio.NewReader(reader)); !errors.Is(err, context.Canceled) {
		t.Fatalf("readLineContext() error = %v, want context.Canceled", err)
	}
}
