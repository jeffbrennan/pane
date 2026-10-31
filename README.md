# diffy

A narrow, keyboard-first terminal viewer for Git changes. Diffs show file headers, line numbers, and added and removed lines without Git's patch metadata. Run `diffy` anywhere inside a Git working tree.

Build with `go build -o diffy .`. Go and Git must be installed. The initial target is macOS.

| Key | Action |
| --- | --- |
| `j` / `k` | Scroll one line |
| `J` / `K` | Jump to the next / previous file |
| `tab` | Switch unstaged / staged view |
| `space` | Stage / unstage the current whole file |
| `w` | Toggle line wrapping |
| `h` / `l` | Scroll horizontally when wrapping is off |
| `c` | Commit staged changes |
| `p` / `P` | Push / pull after confirmation |
| `r` | Refresh |
| `q` | Quit |

The current file is the one at the top of the viewport. Changes are refreshed by background polling approximately every 1.5 seconds, preserving the current file and approximate position. Git operations use the repository's existing configuration; authentication failures and conflicts must be handled outside diffy. Syntax highlighting, partial staging, and conflict resolution are not supported.

Commits include only staged changes if any are staged. Otherwise, diffy stages all non-ignored changes, including untracked files, before committing. If the commit fails, the automatically staged changes remain staged.
