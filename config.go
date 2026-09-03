// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type config struct {
	DeviceName     string `json:"device_name"`
	APIKey         string `json:"api_key"`
	DataDir        string `json:"data_dir,omitempty"`
	CacheDir       string `json:"cache_dir,omitempty"`
	CacheSizeMB    *int   `json:"cache_size_mb,omitempty"`
	PipewireDevice string `json:"pipewire_device,omitempty"`
	InitialVolume  *int   `json:"initial_volume,omitempty"`
	WebSocket      string `json:"websocket,omitempty"`
}

func loadConfig(p paths) (config, error) {
	data, err := os.ReadFile(p.configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config{}, fmt.Errorf("not configured; run 'soloistd setup' first")
		}
		return config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parse %s: %w", p.configFile, err)
	}
	if err := validateConfig(cfg); err != nil {
		return config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	if cfg.DeviceName == "" {
		return errors.New("device_name is required")
	}
	if cfg.APIKey == "" {
		return errors.New("api_key is required")
	}
	if cfg.CacheSizeMB != nil && *cfg.CacheSizeMB != 0 && *cfg.CacheSizeMB < 100 {
		return errors.New("cache_size_mb must be 0 or at least 100")
	}
	if cfg.InitialVolume != nil && (*cfg.InitialVolume < 0 || *cfg.InitialVolume > 100) {
		return errors.New("initial_volume must be between 0 and 100")
	}
	return nil
}

func saveConfig(p paths, cfg config) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.configFile), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(p.configFile), 0o700); err != nil {
		return fmt.Errorf("secure config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(p.configFile), ".config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, p.configFile); err != nil {
		return err
	}
	return os.Chmod(p.configFile, 0o600)
}

func (cfg config) soloistArgs(p paths) []string {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = p.soloistData
	}
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = p.soloistCache
	}
	args := []string{
		"--device-name", cfg.DeviceName,
		"--api-key", cfg.APIKey,
		"--data-dir", dataDir,
		"--cache-dir", cacheDir,
	}
	if cfg.CacheSizeMB != nil {
		args = append(args, "--cache-size", fmt.Sprint(*cfg.CacheSizeMB))
	}
	if cfg.PipewireDevice != "" {
		args = append(args, "--pipewire-device", cfg.PipewireDevice)
	}
	if cfg.InitialVolume != nil {
		args = append(args, "--initial-volume", fmt.Sprint(*cfg.InitialVolume))
	}
	if cfg.WebSocket != "" {
		args = append(args, "--ws", cfg.WebSocket)
	}
	return args
}
