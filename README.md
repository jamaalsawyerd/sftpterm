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
