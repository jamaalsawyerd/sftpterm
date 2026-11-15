# sftpterm

[![CI](https://github.com/jamaalsawyerd/sftpterm/actions/workflows/ci.yml/badge.svg)](https://github.com/jamaalsawyerd/sftpterm/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jamaalsawyerd/sftpterm)](https://github.com/jamaalsawyerd/sftpterm/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/jamaalsawyerd/sftpterm.svg)](https://pkg.go.dev/github.com/jamaalsawyerd/sftpterm)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**A friendly, two-pane file manager for SFTP that runs in your terminal.**
Your computer on the left, the server on the right: browse both, mark some
files, press `c`, and watch them fly across with live progress bars.

If you can `ssh` into a machine, sftpterm can already talk to it. It reads
your `~/.ssh/config`, uses your keys and ssh-agent, and checks host keys the
same way OpenSSH does. There's no server-side setup and nothing extra to
configure.

![sftpterm demo](demo/demo.gif)

## Contents

- [Features](#features)
- [Install](#install)
- [Quick start](#quick-start)
- [Command-line options](#command-line-options)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Guide](#guide)
- [Configuration](#configuration)
- [Security](#security)
- [Development](#development)

## Features

- 🗂️ **Dual-pane browser.** Local files on the left, remote on the right,
  with sorting, filtering, and hidden-file toggles on both.
- 🔑 **Uses your existing SSH setup.** Hosts from `~/.ssh/config`,
  ssh-agent, key files, passwords, and keyboard-interactive login.
- 🚀 **A transfer queue that shows its work.** Live progress bars,
  throughput, and ETA. You can run several files at once and cap upload and
  download speeds, either globally or per host.
- 🛡️ **Safe by default.** Strict `known_hosts` checking, files written under
  a temporary name and swapped in only when complete, and nothing a server
  sends can escape the folder you chose or mess with your terminal.
- 📌 **Remembers where you were.** Bookmarks for local/remote folder pairs,
  and reconnecting reopens the folders you were last in.
- ✏️ **File management on both sides.** New folder, rename, delete (with
  live progress), and quick previews of text and binary files.
- 🎨 **Six themes, four icon sets,** and a little bit of flair.

## Install

**Download a binary** for Linux, macOS, or Windows (amd64 and arm64) from the
[releases page](https://github.com/jamaalsawyerd/sftpterm/releases). Unpack
it and put `sftpterm` somewhere on your `PATH`.

**Or build it with Go** 1.26 or newer:

```sh
go install github.com/jamaalsawyerd/sftpterm/cmd/sftpterm@latest
```

Check that it worked with `sftpterm --version`.

## Quick start

1. **Start it.** Run `sftpterm` on its own to pick from the hosts in your
   `~/.ssh/config`, or press `n` to type in a new one. If you already know
   where you're going, pass it directly: `sftpterm me@myserver`.
2. **Look around.** Use the arrow keys (or `j`/`k`) to move, `enter` to open
   a folder, and `backspace` to go up. `tab` switches between your computer
   and the server.
3. **Copy something.** Press `space` to mark files, then `c` to copy them to
   the folder open on the other side. Progress shows up in the **Transfers**
   panel at the bottom.

Press `?` at any time to see every key that works on the current screen,
and `q` to quit.

## Command-line options

```
sftpterm [options] [destination]
```

### Destination

The destination is optional. Leave it out to choose a host on the connect
screen. Any of these forms work:

| Form | Example | Notes |
| --- | --- | --- |
| ssh config alias | `sftpterm nas` | Uses `HostName`, `User`, `Port`, `IdentityFile`, and friends from that `Host` block in `~/.ssh/config` |
| `[user@]host` | `sftpterm pi@raspberrypi.local` | Any hostname or IP address |
| `[user@]host:path` | `sftpterm me@nas:/srv/media` | scp style; opens the server in `path` |
| `[user@][ipv6]:path` | `sftpterm me@[2001:db8::1]:/srv` | Put IPv6 addresses in brackets |
| `sftp://[user@]host[:port][/path]` | `sftpterm sftp://me@nas:2222/srv` | URL form; `ssh://` works too |

Anything you leave out (user, port, key) comes from your ssh config, then
falls back to your local username and port 22.

### Options

| Option | What it does | Default |
| --- | --- | --- |
| `-P`, `--port <port>` | Port to connect to. Overrides ssh config. Needs a destination. | from ssh config, or `22` |
| `-c`, `--config <path>` | Config file to read, and to save settings to from inside the app | [platform config dir](#where-files-live)`/sftpterm/config.toml` |
| `--theme <name>` | Color theme for this run only: `charm`, `catppuccin-mocha`, `catppuccin-latte`, `dracula`, `tokyo-night`, `nord` | `charm` (or your config) |
| `--debug` | Write a detailed log to `debug.log` in the [cache dir](#where-files-live) | off |
| `-v`, `--version` | Print the version and exit | |
| `-h`, `--help` | Show help and exit | |

### Subcommands

| Command | What it does |
| --- | --- |
| `sftpterm completion <bash\|zsh\|fish\|powershell>` | Print a shell completion script. Run `sftpterm completion <shell> --help` for setup steps. |
| `sftpterm man` | Print a man page, e.g. `sftpterm man > ~/.local/share/man/man1/sftpterm.1` |
| `sftpterm help [command]` | Same as `--help` |

### Examples

```sh
sftpterm                          # pick a host from ~/.ssh/config
sftpterm nas                      # connect to an ssh config alias
sftpterm me@nas:/srv/media        # connect and open /srv/media
sftpterm -P 2222 pi@raspberrypi   # use a non-standard port
sftpterm sftp://me@nas:2222/srv   # the same thing, as a URL
sftpterm --theme dracula nas      # try a theme without changing your config
sftpterm --debug nas              # log what happens, for bug reports
```

## Keyboard shortcuts

The bottom of the screen always shows the most useful keys for whatever has
focus, and `?` expands it to the full list.

### Moving around

| Key | Action |
| --- | --- |
| `↑`/`↓` or `k`/`j` | Move the cursor |
| `pgup`/`pgdn` | Page up / down |
| `g` / `G` | Jump to the top / bottom |
| `enter`, `→`, or `l` | Open a folder (or preview a file) |
| `backspace`, `←`, or `h` | Go up one folder |
| `~` | Go to your home folder |
| `ctrl+r` | Refresh the listing |
| `tab` | Switch between local, remote, and the transfer queue |

### Selecting and viewing

| Key | Action |
| --- | --- |
| `space` | Mark or unmark the item under the cursor |
| `ctrl+a` | Mark or unmark everything |
| `/` | Filter by name (`enter` keeps the filter, `esc` clears it) |
| `.` | Show or hide hidden files |
| `s` | Cycle sort order: name, size, modified |
| `v` | Preview a file |

### Files and transfers

| Key | Action |
| --- | --- |
| `c` or `F5` | Copy marked items (or the one under the cursor) to the other side |
| `n` or `F7` | New folder |
| `r` or `F2` | Rename |
| `d`, `delete`, or `F8` | Delete, after asking |
| `+` / `-` | More or fewer simultaneous transfers |

### Bookmarks, settings, and the app

| Key | Action |
| --- | --- |
| `b` | Bookmark the current local + remote folders |
| `'` or `B` | Open your bookmarks |
| `S` or `L` | Settings (theme, icons, transfers, speed limits) |
| `T` | Switch to the next theme |
| `ctrl+d` | Disconnect |
| `?` | Show all keys |
| `q` or `ctrl+c` | Quit |

### In the Transfers panel

| Key | Action |
| --- | --- |
| `x` | Cancel the selected transfer |
| `X` | Cancel all transfers |
| `r` | Retry a failed or cancelled transfer |
| `c` | Clear finished transfers |

On the connect screen, `enter` connects to the highlighted host and `n`
lets you type in one that isn't in your ssh config.

## Guide

### Transferring files

Mark files with `space` (or `ctrl+a` for everything), then press `c` to copy
them to the directory open on the other side. With nothing marked, `c`
copies the item under the cursor. Folders are copied recursively.

- Progress, speed, and time remaining show in the **Transfers** panel. `tab`
  into it to cancel, retry, or clear items.
- If a file already exists, you choose to **overwrite**, **skip**, or **keep
  both** (saving the new one as `report (1).pdf`). Press the capital letter
  (`O`, `S`, `K`) to use the same answer for the rest of the batch.
- Files are written to a hidden `.name.sftpterm-part` file and renamed into
  place once complete, so an interrupted transfer never leaves a
  half-written file under the real name. Modification times and permissions
  are kept.
- `+`/`-` change how many files transfer at once. Speed limits (in `S`) are
  shared by all transfers going the same direction.

### Managing files

These work the same on both sides:

- `n` creates a folder.
- `r` renames. It never overwrites an existing file.
- `v` or `enter` previews a file: text with line numbers, or a hex dump for
  binary files.
- `d` deletes the marked items (or the one under the cursor) after asking
  for confirmation. While it runs, each item is struck through, the status
  bar shows how far along it is and which file it's on, and items disappear
  from the list as soon as they're gone. That matters most on a slow server
  or a big folder. If you try to quit or disconnect mid-delete, sftpterm asks
  first.

### Bookmarks and remembered locations

- `b` bookmarks the current pair of folders (local and remote) for this
  connection, and `'` lists bookmarks so you can jump both panes at once.
- Bookmarks also appear under each host on the connect screen, so you can
  connect straight into one.
- Reconnecting to a host reopens the folders you were last in.

### Settings

`S` opens settings for the theme, icons, hidden files, simultaneous
transfers, and speed limits. You can apply changes for just this session,
save them as your defaults, or save transfer settings for only the current
host. Saving edits your config file in place and keeps your comments and
formatting.

### Logging in

sftpterm tries these in order:

1. Keys from your ssh-agent
2. Your key files: `IdentityFile` from ssh config, or `~/.ssh/id_ed25519`,
   `id_ecdsa`, and `id_rsa`
3. Password and keyboard-interactive prompts

Encrypted keys only ask for their passphrase if the server actually accepts
that key. Passwords are never stored.

**Not supported yet:** `ProxyJump`/`ProxyCommand` and SSH certificates.

### A little bit of flair

A gradient splash screen greets you (any key skips it), a glowing bar slides
to whichever side has focus, and a big batch of transfers ends in confetti.
Terminals that support it (Windows Terminal, Ghostty, iTerm2, …) also show
overall transfer progress in the tab or taskbar. Set `animations = false` in
your config to turn off the motion.

## Configuration

Everything works out of the box, so you never *need* a config file. To
change the defaults, create `config.toml` in the
[config directory](#where-files-live) (or point `--config` at another file).
Every setting is optional:

```toml
theme = "charm"        # charm, catppuccin-mocha, catppuccin-latte, dracula, tokyo-night, nord
icons = "unicode"      # unicode, nerd, emoji, ascii
show_hidden = false    # show dotfiles
animations = true      # splash screen, sliding focus bar, confetti
bell = false           # ring the terminal bell when a long batch finishes

[transfers]
concurrency = 3            # files transferred at once (1–16)
upload_limit = "unlimited" # total across all uploads, e.g. "10MB/s", "500K"
download_limit = "unlimited"
```

Per-host overrides, keyed by ssh alias, take precedence over `[transfers]`:

```toml
[hosts.nas]
concurrency = 2
upload_limit = "5MB/s"
```

Sizes use binary units (`1 MB` = 1024 KiB). Press `T` in the app to try
themes live before picking one. Use `nerd` icons if your terminal uses a
[Nerd Font](https://www.nerdfonts.com/).

### Where files live

| File | Linux | macOS | Windows |
| --- | --- | --- | --- |
| `config.toml`, `bookmarks.json` | `~/.config/sftpterm/` | `~/Library/Application Support/sftpterm/` | `%AppData%\sftpterm\` |
| `debug.log` (with `--debug`) | `~/.cache/sftpterm/` | `~/Library/Caches/sftpterm/` | `%LocalAppData%\sftpterm\` |

On Linux, `$XDG_CONFIG_HOME` and `$XDG_CACHE_HOME` are respected. Bookmarks
always live in the config directory, even if you use `--config` to load a
config file from somewhere else.

## Security

- **Host keys are always verified** against `~/.ssh/known_hosts` (or
  `UserKnownHostsFile`). A new host shows its fingerprint and asks before
  it's trusted. A **changed** key is refused outright, because that is what
  a man-in-the-middle attack looks like.
- **Servers can't attack your terminal.** Terminal control characters are
  stripped from anything shown from a server (file names, previews, errors).
- **Servers can't write outside the folder you chose.** Names that could
  escape the target folder (like `../x`) are refused.

## Development

```sh
make build   # build ./bin/sftpterm
make run     # build and launch
make test    # run tests with the race detector
make lint    # run golangci-lint
make cover   # coverage across packages
make fuzz    # fuzz the parsers and sanitizers (FUZZTIME=30s each)
make golden  # rewrite UI snapshots after an intentional change
make demo    # re-record demo/demo.gif (needs Docker and VHS)
```

Tests run real SSH and SFTP sessions against an in-process server
(`internal/sshx/sshtest`), and UI snapshots in `internal/ui/testdata` catch
unintended layout changes.

For a throwaway SFTP server to try things against:

```sh
docker run --rm -p 2222:22 atmoz/sftp demo:demo:::upload
sftpterm -P 2222 demo@localhost   # password: demo
```

Releases are built by [GoReleaser](https://goreleaser.com) when a `v*` tag is
pushed.

## License

[MIT](LICENSE)
