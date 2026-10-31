# diffy

A narrow, keyboard-first terminal viewer for Git changes. Diffs show file headers, line numbers, and added and removed lines without Git's patch metadata. Run `diffy` anywhere inside a Git working tree.

Build with `go build -o diffy .`. Go and Git must be installed. The initial target is macOS.

| Key | Action |
| --- | --- |
| `j` / `k` | Scroll one line |
| `}` / `{` | Jump to the next / previous paragraph boundary |
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

The current file is the one at the top of the viewport. Changes are refreshed by background polling approximately every 1.5 seconds, preserving the current file and approximate position. Git operations use the repository's existing configuration; authentication failures and conflicts must be handled outside diffy. Syntax highlighting, partial staging, and conflict resolution are not supported.

Commits include only staged changes if any are staged. Otherwise, diffy stages all non-ignored changes, including untracked files, before committing. If the commit fails, the automatically staged changes remain staged. After a successful commit, diffy opens history with the new commit selected.

In diffs, paragraph boundaries are blank lines, file headers, and hunk separators. In history, `j` / `k` or `}` / `{` select commits, `/` searches author name or email, message, and hash, and Enter opens a read-only diff. Press `Esc` or `q` to go back. Addition and deletion counts omit binary files, which are counted separately. Merge commit counts and diffs are relative to their first parent.
