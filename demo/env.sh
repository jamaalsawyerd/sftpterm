# Sourced by demo.tape (hidden): point sftpterm at the demo environment.
DEMO_DIR=${DEMO_DIR:-/tmp/sftpterm-demo}
export HOME=$DEMO_DIR/home XDG_CONFIG_HOME=$DEMO_DIR/home/.config SSH_AUTH_SOCK=
export PATH="$(pwd)/bin:$PATH"
cd "$HOME"
PS1='$ '
