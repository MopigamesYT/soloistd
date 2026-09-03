// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
	"path/filepath"
)

type paths struct {
	configFile   string
	managerData  string
	soloistData  string
	soloistCache string
	unitFile     string
}

func resolvePaths() (paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return paths{}, err
	}

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(home, ".cache")
	}

	return paths{
		configFile:   filepath.Join(configHome, "soloistd", "config.json"),
		managerData:  filepath.Join(dataHome, "soloistd"),
		soloistData:  filepath.Join(dataHome, "soloist"),
		soloistCache: filepath.Join(cacheHome, "soloist"),
		unitFile:     filepath.Join(configHome, "systemd", "user", "soloistd.service"),
	}, nil
}

func (p paths) soloistBinary() string {
	return filepath.Join(p.managerData, "bin", "soloist")
}

func (p paths) releaseMetadata() string {
	return filepath.Join(p.managerData, "release.json")
}
