# sftpterm

A delightful terminal UI for transferring files over SFTP, built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Lip Gloss](https://github.com/charmbracelet/lipgloss), and friends.

> 🚧 **Work in progress.** Not ready for real use yet.

## Planned features

- Dual-pane browser: local on the left, remote on the right
- Connect using hosts from `~/.ssh/config`, ssh-agent, keys, or passwords
- Strict `known_hosts` verification with a trust-on-first-use prompt
- Transfer queue with live progress bars, throughput, and ETA
- Configurable simultaneous transfers and upload/download speed limits
- Per-connection bookmarks that restore both local and remote directories
- Themes, icons, and a little bit of flair

## Install

```sh
go install github.com/jamaalsawyerd/sftpterm/cmd/sftpterm@latest
```

## Configuration

sftpterm reads `~/.config/sftpterm/config.toml` (or the platform equivalent;
override with `--config`). Every setting is optional:

```toml
theme = "charm"        # charm, catppuccin-mocha, catppuccin-latte, dracula, tokyo-night, nord
icons = "unicode"      # unicode, nerd, emoji, ascii
show_hidden = false

[transfers]
concurrency = 3            # files transferred at once (1–16)
upload_limit = "unlimited" # total across all uploads, e.g. "10MB/s", "500K"
download_limit = "unlimited"
```

Sizes use binary units (`1 MB` = 1024 KiB). Press `T` in the app to try themes
live, and run with `--debug` to write a log to `~/.cache/sftpterm/debug.log`.

## Development

```sh
make build   # build ./bin/sftpterm
make run     # build and launch
make test    # run tests with the race detector
make lint    # run golangci-lint
```

For a local SFTP server to test against:

```sh
docker run --rm -p 2222:22 atmoz/sftp demo:demo:::upload
```

## License

[MIT](LICENSE)
