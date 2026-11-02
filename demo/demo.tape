# Records demo/demo.gif. Run `make demo` (it builds, starts the demo server,
# records, and cleans up).
Output demo/demo.gif

Require docker
Set Shell "bash"
Set FontSize 15
Set Width 1240
Set Height 720
Set Padding 14
Set Framerate 24
Set TypingSpeed 70ms
Set WindowBar Colorful

Hide
Type "source demo/env.sh && clear" Enter
Sleep 500ms
Show

Type "sftpterm" Sleep 400ms Enter
Sleep 2.4s

# Pick "nas" from ~/.ssh/config and log in.
Sleep 600ms Enter
Sleep 1.2s
Type "demo" Sleep 300ms Enter
Sleep 1.5s

# Into the media share on the server.
Enter
Sleep 1.2s

# Over to the local side: copy an album, then a file, across.
Tab Sleep 600ms
Down Sleep 300ms Enter Sleep 900ms
Down Sleep 300ms Type "c" Sleep 1.2s
Backspace Sleep 800ms
Down Down Down Down Sleep 400ms Type "c"
Sleep 1.5s

# Themes while it works.
Type "T" Sleep 1.3s
Type "T" Sleep 1.3s
Type "T" Sleep 3.5s

# Peek at a file.
Up Sleep 400ms Type "v" Sleep 2s Escape Sleep 800ms

# All the keys.
Type "?" Sleep 2.5s Type "?" Sleep 800ms

Type "q"
Sleep 600ms
