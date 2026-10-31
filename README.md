# pane

A narrow, keyboard-first terminal viewer for Git changes. Diffs show file headers, line numbers, and added and removed lines without Git's patch metadata. Run `pane` anywhere inside a Git working tree.

If the working tree and index are clean at startup, pane opens commit history. Otherwise it opens the unstaged diff view.

Install with `go install github.com/jeffbrennan/pane@latest`, or run `go install .` from a checkout; either places `pane` in `$(go env GOPATH)/bin`, which must be on your `PATH`. Go and Git must be installed. The initial target is macOS.

For development, `just b` builds pane, installs it, and runs it in the current repository.

| Key | Action |
| --- | --- |
| `j` / `k` | Scroll one line |
| `gg` / `G` | Jump to the first / last line or history entry |
| `ctrl-d` / `ctrl-u` | Move down / up half a page |
| `ctrl-f` / `ctrl-b` | Move down / up a page |
| `ctrl-e` / `ctrl-y` | Scroll the diff viewport down / up one line |
| `zt` / `zz` / `zb` | Place the active diff line at the top / middle / bottom |
| `}` / `{` | Jump to the next / previous paragraph boundary, or five commits in history |
| `J` / `K` | Jump to the next / previous file, placing it at the top |
| `f` | Find a file in the current diff by fuzzy name |
| `/`, `n` / `N` | Search the diff; jump to the next / previous match |
| `o` | Open the current file at the active line in `$VISUAL` / `$EDITOR` |
| `y` | Copy the current file path, or the commit hash in history |
| `tab` | Switch unstaged / staged view |
| `space` | Stage / unstage the current whole file |
| `w` | Toggle line wrapping |
| `h` / `l`, `zh` / `zl` | Scroll horizontally when wrapping is off |
| `c` | Commit staged changes |
| `p` / `P` | Push / pull after confirmation |
| `r` | Refresh |
| `H` | Open commit history |
| `?` | Show keyboard shortcuts |
| `q` | Quit |

The active line is marked with `▸`; staging applies to the file containing it. The viewport keeps surrounding lines visible as you navigate and pins a file header when its diff scrolls beyond the viewport. Changes are refreshed by background polling approximately every 1.5 seconds, preserving the current file and approximate position. Git operations use the repository's existing configuration; authentication failures and conflicts must be handled outside pane. Syntax highlighting, partial staging, and conflict resolution are not supported.

Commits include only staged changes if any are staged. Otherwise, pane stages all non-ignored changes, including untracked files, before committing. If the commit fails, the automatically staged changes remain staged. After a successful commit, pane opens history with the new commit selected.

In diffs, paragraph boundaries are blank lines, file headers, and hunk separators. Diff search is incremental and case-insensitive unless the query contains an uppercase letter; matches are highlighted and `n` / `N` wrap around. `o` opens the working-tree file, so in a historical commit the line may have moved since. Copying uses `pbcopy`, `wl-copy`, `xclip`, or `xsel`. In history, `j` / `k` select commits, `}` / `{` jump five commits, and `/` searches author name or email, message, and hash. While typing a search, use the arrow keys to select a result; Enter opens the focused commit's read-only diff. Press `Esc` or `q` to go back. Addition and deletion counts omit binary files, which are counted separately. Merge commit counts and diffs are relative to their first parent.

The history landing page shows the branch, local changes, and commits ahead of or behind its configured upstream. Ahead/behind counts use locally stored remote-tracking refs and may be stale; pane does not fetch automatically. History reloads when the checked-out commit changes. Push and pull are available from history as well as the diff view. After a successful push, pane returns to history and refreshes its status.

## License

Pane is licensed under the [MIT License](LICENSE).
