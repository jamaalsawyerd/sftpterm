#!/usr/bin/env bash
# Prepares a throwaway environment for recording the demo GIF:
# a fake $HOME with sample files and an ssh config, and an SFTP server
# (atmoz/sftp in Docker) on 127.0.0.1:2222 with user demo / password demo.
set -euo pipefail

DEMO_DIR=${DEMO_DIR:-/tmp/sftpterm-demo}
PORT=${DEMO_PORT:-2222}
rm -rf "$DEMO_DIR"
mkdir -p "$DEMO_DIR"/{home/.ssh,home/.config/sftpterm,server}

rand() { head -c "$2" /dev/urandom > "$1"; }

# Local files.
H=$DEMO_DIR/home
mkdir -p "$H/Music/Kind of Blue" "$H/Photos/2026"
for t in "So What" "Freddie Freeloader" "Blue in Green" "All Blues"; do
  rand "$H/Music/Kind of Blue/$t.flac" $((4 + RANDOM % 3))000000
done
for i in 01 02 03 04 05 06; do rand "$H/Photos/2026/IMG_00$i.jpg" 900000; done
rand "$H/report.pdf" 2400000
printf 'buy milk\nback up the NAS\ncall mom\n' > "$H/notes.txt"
printf '#!/bin/sh\necho deploying\n' > "$H/deploy.sh" && chmod +x "$H/deploy.sh"

# Remote files.
S=$DEMO_DIR/server
mkdir -p "$S/movies" "$S/backups"
rand "$S/movies/trailer.mp4" 12000000
printf 'Welcome to the media share!\n' > "$S/README.txt"
chmod -R 777 "$S"

cat > "$H/.ssh/config" <<CFG
Host nas
    HostName 127.0.0.1
    Port $PORT
    User demo

Host pi
    HostName raspberrypi.local
    User pi
CFG

cat > "$H/.config/sftpterm/config.toml" <<CFG
# Slow uploads down a little so the progress bars are visible.
[transfers]
upload_limit = "6MB/s"
CFG

docker rm -f sftpterm-demo >/dev/null 2>&1 || true
docker run -d --rm --name sftpterm-demo -p "127.0.0.1:$PORT:22" \
  -v "$S:/home/demo/media" atmoz/sftp demo:demo:1000 >/dev/null

# Wait until sshd answers, then trust its key so the demo skips that prompt.
for _ in $(seq 1 60); do
  if ssh-keyscan -p "$PORT" 127.0.0.1 > "$H/.ssh/known_hosts" 2>/dev/null && [ -s "$H/.ssh/known_hosts" ]; then
    echo "demo ready in $DEMO_DIR"
    exit 0
  fi
  sleep 1
done
echo "sftp server did not start" >&2
exit 1
