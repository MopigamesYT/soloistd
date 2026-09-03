# soloistd

`soloistd` is a small supervisor for [Spotify Soloist](https://developer.spotify.com/documentation/soloist). It stores the API key outside the systemd unit, runs Soloist as a systemd user service, proactively checks for official updates, and retains build-expiry recovery as a fallback.

## Install

### Arch Linux (AUR)

```sh
paru -S soloistd
```

Or build the AUR recipe manually with `makepkg -si`.

### From source

Build and place `soloistd` somewhere permanent on your `PATH`:

```sh
go build -o soloistd .
install -Dm755 soloistd ~/.local/bin/soloistd
```

Configure it. The interactive API-key prompt does not echo:

```sh
soloistd setup
```

The default Spotify Connect device name is `soloistd@<hostname>` (for example, `soloistd@framework13`). Press Enter to accept it or type a different name.

Alternatively, keep the key out of shell history with an environment variable:

```sh
SOLOIST_API_KEY='your-key' soloistd setup --device-name "Kitchen speaker"
```

Install and immediately start the systemd user service:

```sh
soloistd service install
```

If Soloist is already running in a terminal, stop that process first so the service can acquire the same data directory.

Open Spotify on the same local network, open the device picker, and select the configured device. Soloist stores that Spotify Connect session for later restarts.

## Commands

```text
soloistd setup                  Configure the device and fetch Soloist
soloistd run                    Run the foreground supervisor
soloistd pair                   Replace the saved Spotify Connect session
soloistd update                 Fetch the current official Soloist build
soloistd ctl status             Run any `soloist ctl` command
soloistd service status         Show user-service status
soloistd service logs           Follow user-service logs
soloistd service restart        Restart the user service
soloistd service uninstall      Remove only the unit; keep config and data
```

Stop the installed service before using `soloistd pair`, because Soloist permits only one process per data directory.

## Files and security

| Purpose | Default path |
| --- | --- |
| Private configuration | `$XDG_CONFIG_HOME/soloistd/config.json` |
| Managed Soloist binary | `$XDG_DATA_HOME/soloistd/bin/soloist` |
| Installed release metadata | `$XDG_DATA_HOME/soloistd/release.json` |
| Spotify session/state | `$XDG_DATA_HOME/soloist` |
| Playback cache | `$XDG_CACHE_HOME/soloist` |
| systemd user unit | `$XDG_CONFIG_HOME/systemd/user/soloistd.service` |

The usual `~/.config`, `~/.local/share`, and `~/.cache` fallbacks are used when the XDG variables are unset. The config directory is mode `0700` and `config.json` is mode `0600`. The generated unit contains only the path to `soloistd`; it never contains the API key. Soloist itself requires the key as a command-line argument, so the key can still be visible to same-user process inspection while Soloist is running.

Additional documented Soloist options can be set directly in `config.json`: `data_dir`, `cache_dir`, `cache_size_mb`, `pipewire_device`, `initial_volume`, and `websocket`.

## Automatic updates

`soloistd` compares the official archive's CDN metadata at daemon startup and every six hours. When the archive changes, it downloads the stable archive matching the current Linux architecture from `soloist-builds.spotifycdn.com`, extracts and executes `soloist --version` as validation, atomically replaces its user-owned managed executable, and gracefully restarts Soloist. Failed proactive checks do not interrupt playback.

Spotify documents daemon exit status `10` as “build expired.” This remains an independent fallback: `soloistd` unconditionally downloads the current build and restarts whenever Soloist reports status `10`, even if the proactive metadata check failed. It supports `x86_64`, AArch64, and ARMv7.

## License

`soloistd` is licensed under the [Mozilla Public License 2.0](LICENSE).
