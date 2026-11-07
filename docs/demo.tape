# Records the raw demo video. Run `just demo` rather than calling vhs directly:
# it builds pane, prepares a Ghostty clone with one edit in $DEMO_REPO, and
# turns the video into docs/demo.gif with docs/demo_zoom.py. Comments drive
# that script, timed at the next command: `# key: keys = description` shows
# the keypress below the terminal, and `# view: x y width [ease]` eases the
# camera to that view, in fractions of the frame.

Output .scratch/demo-frames/

Set Shell bash
Set FontFamily "Obel Mono Masked"
Set FontSize 22
Set Width 1600
Set Height 900
Set Padding 24
Set Framerate 30
Set TypingSpeed 90ms
Set Theme { "name": "Gruvbox Ember", "background": "#282828", "foreground": "#e8dcb7", "cursor": "#e8604a", "selection": "#504945", "black": "#282828", "red": "#e8604a", "green": "#8aa499", "yellow": "#f1bf4f", "blue": "#a9a273", "magenta": "#c98a9f", "cyan": "#96b4a8", "white": "#e8dcb7", "brightBlack": "#6b6259", "brightRed": "#f27a63", "brightGreen": "#a6c0b2", "brightYellow": "#f7ce74", "brightBlue": "#c5bd8d", "brightMagenta": "#d7a7b8", "brightCyan": "#a8c7bb", "brightWhite": "#fbf1c7" }

Hide
Type "export PATH=$PWD:$PATH PANE_THEME=dark PS1='$ ' && cd $DEMO_REPO && clear"
Enter
Sleep 500ms
Show

# Working tree: stage the hunk and commit it.
Sleep 800ms
Type@110ms "pane"
Sleep 150ms
Enter
# view: 0 0 0.7
Sleep 2.5s
# key: s = stage hunk
Type "s"
Sleep 1.1s
# key: c = commit
Type "c"
# view: 0 0 0.8
Sleep 1s
Type@75ms "fastmem:"
Sleep 250ms
Type@65ms " fix doc"
Sleep 150ms
Type@70ms " comment typo"
Sleep 900ms
# key: enter = commit
Enter
# view: 0 0 1
Sleep 2.5s

# History opens on the new commit; then an older commit with its message.
# key: j j = down
Type@400ms "jj"
Sleep 600ms
# key: enter = open commit
Enter
Sleep 2s
# key: J = jump to the files
Type "J"
Sleep 2.2s
# key: q q q = back out
Type@180ms "qqq"
Sleep 700ms

# Hotspots: tree, files, authors, then a profile.
Type@100ms "pane hot"
Sleep 150ms
Enter
Sleep 3s
# key: j j j = down
Type@400ms "jjj"
Sleep 1s
# key: tab = files
Tab
Sleep 1.7s
# key: tab = authors
Tab
Sleep 2s
# key: p = profile
Type "p"
Sleep 1.6s
# view: 0.02 0.1 0.5 1.4
Sleep 3.2s
# view: 0 0 1 0.9
Sleep 1.2s

Hide
Type "qqq"
