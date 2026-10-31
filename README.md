# pane

A narrow, keyboard-first terminal viewer for Git changes. Diffs show file headers, line numbers, and added and removed lines without Git's patch metadata. Run `pane` anywhere inside a Git working tree.

If the working tree and index are clean at startup, pane opens commit history. Otherwise it opens the unstaged diff view.

Build with `go build -o pane .`. Go and Git must be installed. The initial target is macOS.

For development, `just b` builds and runs pane in the current repository.

| Key | Action |
| --- | --- |
| `j` / `k` | Scroll one line |
| `}` / `{` | Jump to the next / previous paragraph boundary, or five commits in history |
| `J` / `K` | Jump to the next / previous file |
| `tab` | Switch unstaged / staged view |
| `space` | Stage / unstage the current whole file |
| `w` | Toggle line wrapping |
| `h` / `l` | Scroll horizontally when wrapping is off |
| `c` | Commit staged changes |
| `p` / `P` | Push / pull after confirmation |
| `r` | Refresh |
| `H` | Open commit history |
| `?` | Show keyboard shortcuts |
| `q` | Quit |

The active line is marked with `▸`; staging applies to the file containing it. The viewport keeps surrounding lines visible as you navigate. Changes are refreshed by background polling approximately every 1.5 seconds, preserving the current file and approximate position. Git operations use the repository's existing configuration; authentication failures and conflicts must be handled outside pane. Syntax highlighting, partial staging, and conflict resolution are not supported.

Commits include only staged changes if any are staged. Otherwise, pane stages all non-ignored changes, including untracked files, before committing. If the commit fails, the automatically staged changes remain staged. After a successful commit, pane opens history with the new commit selected.

In diffs, paragraph boundaries are blank lines, file headers, and hunk separators. In history, `j` / `k` select commits, `}` / `{` jump five commits, and `/` searches author name or email, message, and hash. While typing a search, use the arrow keys to select a result; Enter opens the focused commit's read-only diff. Press `Esc` or `q` to go back. Addition and deletion counts omit binary files, which are counted separately. Merge commit counts and diffs are relative to their first parent.
