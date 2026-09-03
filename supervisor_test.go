// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunDaemonRefreshesExitTenAndRestarts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Soloist is Linux-only")
	}
	archive, err := archiveName(runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	newBinary := []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'soloist test build'; exit 0; fi\nexit 0\n")
	downloadBody := makeArchive(t, newBinary)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+archive {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			http.Error(w, "metadata temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(downloadBody)
	}))
	defer server.Close()
	oldURL := downloadBaseURL
	downloadBaseURL = server.URL + "/"
	defer func() { downloadBaseURL = oldURL }()

	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	p, err := resolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(p, config{DeviceName: "Test device", APIKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p.soloistBinary()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.soloistBinary(), []byte("#!/bin/sh\nexit 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	a := app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr}
	if err := a.runDaemon(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "exit 10") {
		t.Fatalf("stderr %q does not report expiry", stderr.String())
	}
	installed, err := os.ReadFile(p.soloistBinary())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(installed, newBinary) {
		t.Fatal("managed executable was not replaced by downloaded build")
	}
}

func TestCheckForUpdateDownloadsOnlyChangedRelease(t *testing.T) {
	archive, err := archiveName(runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	newBinary := []byte("#!/bin/sh\necho 'soloist proactive build'\n")
	downloadBody := makeArchive(t, newBinary)
	getCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+archive {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"v2"`)
		if r.Method == http.MethodGet {
			getCount++
			_, _ = w.Write(downloadBody)
		}
	}))
	defer server.Close()
	oldURL := downloadBaseURL
	downloadBaseURL = server.URL + "/"
	defer func() { downloadBaseURL = oldURL }()

	p := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(p.soloistBinary()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.soloistBinary(), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveReleaseMetadata(p, releaseMetadata{Archive: archive, ETag: `"v1"`}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	a := app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &bytes.Buffer{}}
	updated, err := a.checkForUpdate(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !updated || getCount != 1 {
		t.Fatalf("first check: updated=%v GETs=%d, want true and 1", updated, getCount)
	}
	updated, err = a.checkForUpdate(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if updated || getCount != 1 {
		t.Fatalf("second check: updated=%v GETs=%d, want false and 1", updated, getCount)
	}
}

func TestRunDaemonPeriodicallyUpdatesAndRestarts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Soloist is Linux-only")
	}
	archive, err := archiveName(runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	newBinary := []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'soloist periodic build'; exit 0; fi\necho 'new build ran'\nexit 0\n")
	downloadBody := makeArchive(t, newBinary)
	headCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+archive {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			headCount++
			if headCount == 1 {
				w.Header().Set("ETag", `"v1"`)
			} else {
				w.Header().Set("ETag", `"v2"`)
			}
			return
		}
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write(downloadBody)
	}))
	defer server.Close()
	oldURL := downloadBaseURL
	oldInterval := updateCheckInterval
	downloadBaseURL = server.URL + "/"
	updateCheckInterval = 20 * time.Millisecond
	defer func() {
		downloadBaseURL = oldURL
		updateCheckInterval = oldInterval
	}()

	p := testPaths(t)
	if err := saveConfig(p, config{DeviceName: "Test device", APIKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p.soloistBinary()), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBinary := []byte("#!/bin/sh\ntrap 'exit 0' TERM\nwhile :; do sleep 0.01; done\n")
	if err := os.WriteFile(p.soloistBinary(), oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveReleaseMetadata(p, releaseMetadata{Archive: archive, ETag: `"v1"`}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	a := app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr}
	if err := a.runDaemon(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Restarting Soloist with the new build") || !strings.Contains(stdout.String(), "new build ran") {
		t.Fatalf("stdout does not show update restart:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}

func TestServiceUnitQuotesExecutableAndContainsNoSecret(t *testing.T) {
	unit := serviceUnit(`/home/mopi/My Tools/soloistd`)
	if !strings.Contains(unit, `ExecStart="/home/mopi/My Tools/soloistd" run`) {
		t.Fatalf("unit has incorrect ExecStart:\n%s", unit)
	}
	if strings.Contains(strings.ToLower(unit), "api-key") {
		t.Fatal("unit must not contain the API key option")
	}
}

func makeArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	gz := gzip.NewWriter(&result)
	tw := tar.NewWriter(gz)
	header := &tar.Header{Name: "soloist", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func testPaths(t *testing.T) paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	p, err := resolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func Example_archiveName() {
	name, _ := archiveName("amd64")
	fmt.Println(name)
	// Output: soloist_release_x86_64.tar.gz
}
