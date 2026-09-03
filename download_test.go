// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveName(t *testing.T) {
	tests := map[string]string{
		"amd64": "soloist_release_x86_64.tar.gz",
		"arm64": "soloist_release_arm64.tar.gz",
		"arm":   "soloist_release_arm32.tar.gz",
	}
	for arch, want := range tests {
		got, err := archiveName(arch)
		if err != nil {
			t.Fatalf("archiveName(%q): %v", arch, err)
		}
		if got != want {
			t.Fatalf("archiveName(%q) = %q, want %q", arch, got, want)
		}
	}
	if _, err := archiveName("riscv64"); err == nil {
		t.Fatal("expected unsupported architecture to fail")
	}
}

func TestSameReleaseUsesStrongestMetadata(t *testing.T) {
	base := releaseMetadata{Archive: "soloist.tar.gz", ETag: `"abc"`, LastModified: "yesterday", ContentLength: 10}
	if !sameRelease(base, releaseMetadata{Archive: "soloist.tar.gz", ETag: `"abc"`, LastModified: "today", ContentLength: 20}) {
		t.Fatal("matching ETags should identify the same release")
	}
	if sameRelease(base, releaseMetadata{Archive: "soloist.tar.gz", ETag: `"different"`}) {
		t.Fatal("different ETags should identify a changed release")
	}
	if sameRelease(base, releaseMetadata{Archive: "other.tar.gz", ETag: `"abc"`}) {
		t.Fatal("different architecture archives cannot be the same release")
	}
}

func TestExtractSoloist(t *testing.T) {
	payload := []byte("#!/bin/sh\necho soloist test\n")
	archive := tarArchive(t, &tar.Header{Name: "soloist", Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg}, payload)
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "soloist")
	if err := extractSoloist(gz, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("extracted payload = %q, want %q", got, payload)
	}
}

func TestExtractSoloistRejectsSymlink(t *testing.T) {
	archive := tarArchive(t, &tar.Header{Name: "soloist", Typeflag: tar.TypeSymlink, Linkname: "/bin/sh"}, nil)
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if err := extractSoloist(gz, filepath.Join(t.TempDir(), "soloist")); err == nil {
		t.Fatal("expected symlink executable to be rejected")
	}
}

func tarArchive(t *testing.T, header *tar.Header, payload []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 0 {
		if _, err := tw.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}
