# pane

A narrow, keyboard-first terminal viewer for Git changes. Diffs show file headers, line numbers, and added and removed lines without Git's patch metadata. Run `pane` anywhere inside a Git working tree.

Pane detects terminal background changes when supported, falling back to `COLORFGBG`. Set `PANE_THEME=light` or `PANE_THEME=dark` to override detection, or press `T` to choose the opposite theme for the current session.

If the working tree and index are clean at startup, pane opens commit history. Otherwise it opens the unstaged diff view.

Run `pane <commit>` to open a commit's read-only diff directly. A full or abbreviated hash, branch, tag, or other Git commit revision is accepted. Press `Esc` or `q` to return to history.

Run `pane <start>..<end>` for a continuous review of a Git commit range, for example `pane HEAD~3..HEAD`. Git's usual range semantics apply: the start is excluded and the end is included. Symmetric ranges such as `main...feature` are also supported.

Pipe commit lines into pane for a continuous read-only review, for example `git log --oneline -n 10 | pane`. Commits appear in input order with a pinned commit heading and file header. The first field of each nonempty line must be a commit hash. Pane loads the first and last commits first, then fills in the rest in the background; invalid hashes are shown in place. Search covers the diffs loaded so far. The file picker shows files in the commit containing the active line. Deleted files are expanded, and context expansion is not available in this view; normal diff navigation works across commits. Press `Esc` or `q` to return to history.

Install with `go install github.com/jeffbrennan/pane@latest`, or run `go install .` from a checkout; either places `pane` in `$(go env GOPATH)/bin`, which must be on your `PATH`. Go and Git must be installed. The initial target is macOS.

For development, `just b` builds pane, installs it, and runs it in the current repository.

| Key | Action |
| --- | --- |
| `j` / `k` | Scroll one line |
| `gg` / `G` | Jump to the first / last line or history entry |
| `10j`, `3}`, etc. | Repeat a motion with a numeric count |
| `42gg` / `42G` | Jump to the closest visible source line in the current file, or history entry 42 |
| `gH` / `gM` / `gL` | Move to the top / middle / bottom of the viewport |
| `[c` / `]c` | Jump to the previous / next change block in a diff |
| `ctrl-o` / `gi` | Jump back / forward in the diff jump list |
| `ctrl-d` / `ctrl-u` | Move down / up half a page |
| `ctrl-f` / `ctrl-b` | Move down / up a page |
| `ctrl-e` / `ctrl-y` | Scroll the diff viewport down / up one line |
| `zt` / `zz` / `zb` | Place the active diff line at the top / middle / bottom |
| `}` / `{` | Jump to the next / previous paragraph boundary, or five commits in history |
| `J` / `K` | Jump to the next / previous file, placing it at the top |
| `f` | Open file tree; `j` / `k` navigate, `gg` / `G` jump to first / last, `{` / `}` jump between directories, `ctrl-d` / `ctrl-u` and `ctrl-f` / `ctrl-b` move by half / full pages, `h` / `l` fold directories, `/` searches. Relative row numbers help with navigation counts. |
| `/`, `n` / `N` | Search the diff; jump to the next / previous match |
| Search / filter input: `←` / `→`, `home` / `end`, `alt-b` / `alt-f`, `ctrl-a` / `ctrl-e` | Move the query cursor by character, word, or to the start / end |
| Search / filter input: `backspace` / `delete`, `ctrl-w` / `alt-backspace` | Delete a character or the previous word |
| `g?` | Search the diff backward; `n` / `N` follow / reverse the search direction |
| `o` | Open the current file at the active line in `$VISUAL` / `$EDITOR` |
| `y` | Copy the current file path, or the commit hash in history |
| `tab` | Switch unstaged / staged view |
| `space` | Stage / unstage the current whole file |
| `s` | Stage / unstage the current hunk, or the whole file on its header |
| `d` | Discard the current unstaged hunk, or the whole file on its header (confirmation required) |
| `w` | Toggle line wrapping |
| `B` | Toggle short paths (parent directory and filename) |
| `e` | Expand / collapse a deleted file |
| `h` / `l` | On a file header, collapse / reopen the file; on a diff line or `···`, hide / reveal nearby context |
| `zh` / `zl` | Scroll horizontally when wrapping is off |
| `c` | Commit staged changes |
| Commit input: `esc`, `i` / `a`, `I` / `A` | Switch to normal mode; insert before / after the cursor or at the start / end |
| Commit input: `ctrl-g` | Write the message in `$VISUAL` / `$EDITOR` and commit when it closes |
| Commit input normal mode: `h` / `l`, `w` / `b`, `0` / `$` | Move by character, word, or to the start / end |
| Commit input normal mode: `x`, `dw`, `dd`, `D` | Delete a character, word, whole subject, or to the end |
| Commit input: `←` / `→`, `ctrl-b` / `ctrl-f`, `alt-b` / `alt-f`, `home` / `end`, `ctrl-a` / `ctrl-e` | Move the subject cursor by character, word, or to the start / end |
| Commit input: `backspace` / `delete`, `ctrl-d`, `ctrl-w` / `alt-backspace`, `ctrl-u` / `ctrl-k` | Delete a character, previous word, or text to the start / end |
| `p` / `P` | Push / pull |
| `r` | Refresh |
| `H` | Open commit history |
| `u` | In history, edit the latest unpublished commit's message |
| `?` | Show keyboard shortcuts |
| `q` | Quit |

The active line is marked with `▸`. Use `s` to stage or unstage its hunk; untracked and deleted files are staged or unstaged as whole files. The viewport keeps surrounding lines visible as you navigate and pins a file header when its diff scrolls beyond the viewport. Changes are refreshed by background polling approximately every 1.5 seconds, preserving the current file and approximate position. Syntax highlighting uses the terminal's color palette for Go, Python, SQL, Rust, Scala, Java, Kotlin, R, Julia, Groovy, JavaScript, TypeScript, shell scripts, PowerShell, JSON (including notebooks and Avro schemas), YAML, TOML, INI, properties, HCL, Terraform, XML, Protocol Buffers, Dockerfiles, and Makefiles. Added and removed lines use subtle dark-background tints, with stronger highlights for changes within paired lines. Git operations use the repository's existing configuration; authentication failures and conflicts must be handled outside pane. Line-level staging and conflict resolution are not supported.

Commits include only staged changes if any are staged. Otherwise, pane stages all non-ignored changes, including untracked files, before committing. The commit subject starts in insert mode; press `esc` for normal mode, and `esc` again to cancel. Enter commits in either mode; `ctrl-c` cancels directly. Pasting a multiline message puts the first line in the editable subject and preserves the remaining lines as the body. If the commit fails, the automatically staged changes remain staged. After a successful commit, pane opens history with the new commit selected.

Press `ctrl-g` in the commit dialog to edit the full message in `$VISUAL` or `$EDITOR`. Saving and closing the editor submits the commit; lines beginning with `#` are ignored, and an empty message aborts. This also works when editing the latest commit's message from history, subject to the same remote checks.

Detected renames show the old and new paths on separate lines in staged changes and commit views, with removed path text highlighted in red and added text in green. Press `B` to shorten displayed paths to the parent directory and filename; an ellipsis indicates omitted directories and is highlighted when they differ. The file picker displays the new path and can search either full name. Unstaged moves appear as a deletion and an untracked file, as in Git status.

In diffs, paragraph boundaries are blank lines, file headers, and hunk separators. On a file header, `h` collapses the file and `l` reopens its standard diff. On a diff line or `···`, `l` reveals nearby unchanged lines in chunks; repeat to see more, or use `h` to undo a nearby expansion. This also works for staged changes and historical commits. In the unstaged view, `d` discards the Git hunk containing the selected line, or all unstaged changes in the selected file when on its header. Discarding an untracked file deletes it from disk. Pane asks for confirmation; staged changes must be unstaged first. Diff search is incremental and case-insensitive unless the query contains an uppercase letter; matches are highlighted and `n` / `N` wrap around. The diff jump list tracks searches and larger jumps within the current view. `gi` is used for jump-forward because terminals commonly send `ctrl-i` as Tab, which switches staged views. `o` opens the working-tree file, so in a historical commit the line may have moved since. Copying uses `pbcopy`, `wl-copy`, `xclip`, or `xsel`. In history, `j` / `k` select commits, `}` / `{` jump five commits, and `/` searches author name or email, message, and hash. Enter or Space opens the selected commit's read-only diff. While typing a search, use the arrow keys to select a result and Enter to open it. Press `Esc` or `q` to go back. If you opened a file from a diff's file picker, back returns to the picker first; back again returns to history. Addition and deletion counts omit binary files, which are counted separately. Merge commit counts and diffs are relative to their first parent.

While editing a diff or history search, `ctrl-b` / `ctrl-f` move by character, `ctrl-d` deletes the next character, and `ctrl-u` / `ctrl-k` delete to the start / end. In the file picker those keys retain their list-navigation behavior while filtering; use the arrow keys to move the query cursor and the other editing shortcuts above.

The history landing page shows the branch, local changes, and commits ahead of or behind its configured upstream. Ahead/behind counts use locally stored remote-tracking refs and may be stale; pane does not fetch automatically. History reloads when the checked-out commit changes. Push and pull are available from history as well as the diff view. After a successful push, pane returns to history and refreshes its status.

In history, `u` edits the latest commit's subject without changing its body, contents, or staged changes; use `ctrl-g` to edit the body as well. Before rewriting, pane checks the current branch and tag tips on every configured remote. It refuses if the commit is reachable from one, if a remote cannot be contacted, or if a remote's history is not available locally; fetch the remote's branches and tags and try again in the latter case. With no configured remotes, pane permits the edit. Remote state can still change between the check and the rewrite.

## License

Pane is licensed under the [MIT License](LICENSE).
