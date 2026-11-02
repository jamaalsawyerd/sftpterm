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
- Per-connection bookmarks that restore both local and remote directories,
  and reconnecting picks up where you left off
- Themes, icons, and a little bit of flair

## Usage

```sh
sftpterm                          # pick a host from ~/.ssh/config
sftpterm nas                      # connect to an ssh config alias
sftpterm jsd@nas:/srv/media       # scp-style destination with a start directory
sftpterm -P 2222 pi@raspberrypi   # explicit port
sftpterm sftp://jsd@nas:2222/srv  # URL form
```

Press `tab` to switch between the local and remote sides, `n` on the connect
screen to type in a host that isn't in your ssh config, `ctrl+d` to
disconnect, and `?` for every key.

### Transferring files

Mark files with `space` (or `ctrl+a` for everything), then press `c` to copy
them to the directory open on the other side. Without marks, `c` copies the
item under the cursor. Folders are copied recursively.

- Progress, speed, and time remaining show in the **Transfers** panel; `tab`
  into it to cancel (`x`), retry (`r`), or clear finished items (`c`).
- If a file already exists you can overwrite, skip, or keep both
  (`report (1).pdf`); use the capital letter to apply that to the rest of the
  batch.
- Files are written to a hidden `.name.sftpterm-part` file and renamed into
  place when complete, so an interrupted transfer never leaves a half-written
  file under the real name. Modification times and permissions are kept.
- `+`/`-` change how many files transfer at once; `L` sets upload and
  download speed limits. Limits are shared by all transfers in that direction.

### Bookmarks and settings

- `b` bookmarks the current pair of folders (local and remote) for this
  connection; `'` lists bookmarks to jump both panes at once. Bookmarks also
  appear under each host on the connect screen, so you can connect straight
  into one.
- Reconnecting to a host reopens the folders you were last in.
- `S` opens settings: theme, icons, hidden files, simultaneous transfers, and
  speed limits. Apply them for this session, save them as your defaults, or
  save transfer settings for just the current host.

Bookmarks and last locations live in `bookmarks.json` next to the config file.

### Managing files

On either side: `n` new folder, `r` rename, `d` delete (marked items or the
one under the cursor, always with a confirmation), and `v` or `enter` to
preview a file. Previews show text with line numbers, or a hex dump for
binary files. Rename never overwrites an existing file.

Anything shown from a server (file names, previews, errors) has terminal
control characters stripped, and names that could escape the target folder
(like `../x`) are refused, so a hostile server can't write outside the
directory you chose or hijack your terminal.

### A little bit of flair

A gradient splash greets you (any key skips it), a glowing bar slides to
whichever side has focus, and a big batch of transfers ends in confetti.
Terminals that support it (Windows Terminal, Ghostty, iTerm2, …) also show
overall transfer progress in the tab or taskbar. Set `animations = false` to
turn off the motion.

### Authentication and host keys

sftpterm tries, in order: keys from your ssh-agent, your key files
(`IdentityFile` from ssh config, or `~/.ssh/id_ed25519`, `id_ecdsa`,
`id_rsa`), then password and keyboard-interactive prompts. Encrypted keys only
ask for their passphrase if the server actually accepts that key.

Host keys are always verified against `~/.ssh/known_hosts` (or
`UserKnownHostsFile`). A new host shows its fingerprint and asks before being
trusted; a **changed** key is refused outright, since that is what a
man-in-the-middle attack looks like. Passwords are never stored.

Not supported yet: `ProxyJump`/`ProxyCommand` and certificates.

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
animations = true      # splash screen, sliding focus bar, confetti
bell = false           # ring the terminal bell when a long batch finishes

[transfers]
concurrency = 3            # files transferred at once (1–16)
upload_limit = "unlimited" # total across all uploads, e.g. "10MB/s", "500K"
download_limit = "unlimited"
```

Per-host overrides (by ssh alias) take precedence over `[transfers]`:

```toml
[hosts.nas]
concurrency = 2
upload_limit = "5MB/s"
```

Saving settings from the app edits this file in place, keeping your comments
and formatting.

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
