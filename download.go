// SPDX-License-Identifier: MPL-2.0

package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var downloadBaseURL = "https://soloist-builds.spotifycdn.com/"

type releaseMetadata struct {
	Archive       string `json:"archive"`
	ETag          string `json:"etag,omitempty"`
	LastModified  string `json:"last_modified,omitempty"`
	ChecksumCRC32 string `json:"checksum_crc32c,omitempty"`
	ContentLength int64  `json:"content_length,omitempty"`
}

func archiveName(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "soloist_release_x86_64.tar.gz", nil
	case "arm64":
		return "soloist_release_arm64.tar.gz", nil
	case "arm":
		return "soloist_release_arm32.tar.gz", nil
	default:
		return "", fmt.Errorf("Soloist has no Linux download for architecture %q", goarch)
	}
}

func (a app) installLatest(ctx context.Context, p paths) error {
	archive, err := archiveName(runtime.GOARCH)
	if err != nil {
		return err
	}
	binDir := filepath.Dir(p.soloistBinary())
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create binary directory: %w", err)
	}
	tmpDir, err := os.MkdirTemp(binDir, ".update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	url := downloadBaseURL + archive
	fmt.Fprintf(a.stdout, "Downloading current Spotify Soloist build for %s...\n", runtime.GOARCH)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "soloistd/"+version)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download Soloist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Soloist: server returned %s", resp.Status)
	}

	limited := &io.LimitedReader{R: resp.Body, N: 256 << 20}
	gz, err := gzip.NewReader(limited)
	if err != nil {
		return fmt.Errorf("read Soloist archive: %w", err)
	}
	defer gz.Close()
	tmpBinary := filepath.Join(tmpDir, "soloist")
	if err := extractSoloist(gz, tmpBinary); err != nil {
		return err
	}
	if limited.N == 0 {
		return errors.New("Soloist archive exceeds 256 MiB limit")
	}
	if err := os.Chmod(tmpBinary, 0o755); err != nil {
		return err
	}

	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(checkCtx, tmpBinary, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("downloaded Soloist failed validation: %w", err)
	}
	if !strings.HasPrefix(string(out), "soloist ") {
		return fmt.Errorf("downloaded file returned unexpected version output")
	}
	if err := os.Rename(tmpBinary, p.soloistBinary()); err != nil {
		return fmt.Errorf("install Soloist: %w", err)
	}
	metadata := metadataFromResponse(archive, resp)
	if err := saveReleaseMetadata(p, metadata); err != nil {
		return fmt.Errorf("record installed Soloist release: %w", err)
	}
	fmt.Fprintf(a.stdout, "Installed %s", out)
	return nil
}

func (a app) checkForUpdate(ctx context.Context, p paths) (bool, error) {
	archive, err := archiveName(runtime.GOARCH)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, downloadBaseURL+archive, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "soloistd/"+version)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("check for Soloist update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("check for Soloist update: server returned %s", resp.Status)
	}

	remote := metadataFromResponse(archive, resp)
	installed, err := loadReleaseMetadata(p)
	if err == nil && fileIsExecutable(p.soloistBinary()) && sameRelease(installed, remote) {
		return false, nil
	}
	fmt.Fprintln(a.stdout, "A newer or untracked Spotify Soloist build is available.")
	if err := a.installLatest(ctx, p); err != nil {
		return false, err
	}
	return true, nil
}

func metadataFromResponse(archive string, resp *http.Response) releaseMetadata {
	return releaseMetadata{
		Archive:       archive,
		ETag:          resp.Header.Get("ETag"),
		LastModified:  resp.Header.Get("Last-Modified"),
		ChecksumCRC32: resp.Header.Get("X-Amz-Checksum-Crc32c"),
		ContentLength: resp.ContentLength,
	}
}

func sameRelease(installed, remote releaseMetadata) bool {
	if installed.Archive == "" || installed.Archive != remote.Archive {
		return false
	}
	if installed.ETag != "" && remote.ETag != "" {
		return installed.ETag == remote.ETag
	}
	if installed.ChecksumCRC32 != "" && remote.ChecksumCRC32 != "" {
		return installed.ChecksumCRC32 == remote.ChecksumCRC32
	}
	if installed.LastModified != "" && remote.LastModified != "" {
		return installed.LastModified == remote.LastModified && installed.ContentLength == remote.ContentLength
	}
	return false
}

func loadReleaseMetadata(p paths) (releaseMetadata, error) {
	data, err := os.ReadFile(p.releaseMetadata())
	if err != nil {
		return releaseMetadata{}, err
	}
	var metadata releaseMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return releaseMetadata{}, err
	}
	return metadata, nil
}

func saveReleaseMetadata(p paths, metadata releaseMetadata) error {
	if err := os.MkdirAll(p.managerData, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(p.managerData, ".release-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p.releaseMetadata())
}

func fileIsExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

func extractSoloist(src io.Reader, destination string) error {
	tr := tar.NewReader(src)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read Soloist archive: %w", err)
		}
		name := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(header.Name)), "./")
		if name != "soloist" {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return errors.New("Soloist archive executable is not a regular file")
		}
		if header.Size < 1 || header.Size > 512<<20 {
			return fmt.Errorf("Soloist executable has invalid size %d", header.Size)
		}
		out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(out, tr, header.Size)
		closeErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("extract Soloist: %w", copyErr)
		}
		if closeErr != nil {
			return closeErr
		}
		return nil
	}
	return errors.New("Soloist archive does not contain a 'soloist' executable")
}
