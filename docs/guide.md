# Pane guide

The reference for pane's screens, keys, and behavior. For a short overview,
see [README.md](../README.md).

## Starting pane

### Working tree

Run `pane` anywhere inside a Git working tree. If the working tree and index
are clean at startup, pane opens commit history. Otherwise it opens the
unstaged diff view. Changes are refreshed by background polling about every
second, or every three seconds while the terminal is unfocused, preserving the
current file and approximate position. Regaining focus refreshes immediately.

Pane detects terminal background changes when supported, falling back to
`COLORFGBG`. Set `PANE_THEME=light` or `PANE_THEME=dark` to override detection,
or press `T` to choose the opposite theme for the current session.

### Commits and ranges

`pane <commit>` opens a commit's read-only diff directly. A full or abbreviated
hash, branch, tag, or other Git commit revision is accepted. Large commits open
as soon as the first files load; the rest loads in the background. Until
loading finishes, search covers loaded files and picker jumps to unloaded files
wait for them.

`pane <start>..<end>` starts a continuous review of a Git commit range, for
example `pane HEAD~3..HEAD`. The start is excluded and the end is included.
Commits are reviewed oldest first, in the order they were written. Symmetric
ranges such as `main...feature` are also supported.

`pane --base main`, `pane -b main`, or `pane main...` opens one read-only
review from the merge base of `main` and `HEAD` to the working tree.
`pane -b` with no revision uses `@{upstream}`, falling back to `origin`'s
default branch. It includes committed, staged, unstaged, and untracked
changes, and refreshes as the working tree changes.

### File history

`pane log -- <path>` or `pane -- <path>` opens a file's history, following
renames. From a diff, `L` opens the history for that file and `Esc` returns to
the original view. Opening a commit selects that file in the diff; change
counts describe the whole commit, not just the selected file. To review the
commits continuously instead, pipe `git log --format=%H --follow -- <path>`
into pane.

### Piped review

Pipe commit lines into pane for a continuous read-only review, for example
`git log --oneline -n 10 | pane`. Commits appear in input order with a pinned
commit heading and file header. The first field of each nonempty line must be a
commit hash. Pane loads the first and last commits first, then fills in the rest
in the background; a large commit appears as its first files load, and invalid
hashes are shown in place. Search covers the diffs loaded so far. The file
picker shows files in the commit containing the active line. Deleted files are
expanded, and context expansion is not available in this view; normal diff
navigation works across commits. Press `Esc` or `q` to return to history.

### Hotspots

`pane hot` (optionally `--since 6mo`) explores an unfamiliar repository.
Activity is scored by attention, not raw churn: each commit's weight is damped
by its size and by how many files it touched, with a 90-day half-life, so a
fixture dump does not outrank a file people keep editing. The tree opens one
level deep and merges single-child directories. Rows show the activity bar,
distinct commits, people, last change, and the top two owners, with a detail
line for the selection. Bots are not people to ask: they stay listed and dimmed
in the authors view, but they are left out of the people counts and out of the
owners column unless an area has no human author at all. Automation is
recognised by convention — a `[bot]` suffix, or a word such as `bot`, `ci`, or
`automation` in the name. An account a project runs under an ordinary-looking
handle carries no marker in Git and counts as a person. The hotspot keys below
cycle the window and reveal generated and vendored files. Tests and docs rank
below source, and paths deleted inside the window are dropped.

Results are cached in `~/Library/Caches/pane` as per-commit numstat, so
reopening is instant and after HEAD moves only the new commits are read.

## Keys

Sections follow the same order as the in-app help: the current screen first,
then the shared motion, finding, acting, and toggle grammar.

### Changes

| Key | Action |
| --- | --- |
| `tab` | Switch unstaged / staged |
| `space` | Stage / unstage file |
| `s` | Stage / unstage hunk |
| `S` | Stash all changes, including untracked |
| `d` | Discard hunk / file |
| `c` | Commit changes |
| `p / P` | Push / pull |
| `F` | Fetch remote changes |
| `r` | Refresh |

### History

| Key | Action |
| --- | --- |
| `H` | Open commit history |
| `↵ / space` | Open selected commit |
| `A` | Edit latest unpublished commit message |
| `S` | Open stashes |
| `b` | Open branches (tab switches to worktrees) |
| `%` | Open hotspots for the last year |
| `R` | Review from this commit to the working tree |
| `V` | Select a commit range to review |
| `m` | Collapse / expand merged commits |

In a commit, `)` and `(` open the next and previous commit without leaving the
view. In a range review they move to the next and previous commit's header,
and a count such as `3)` jumps three commits.

### Stashes

| Key | Action |
| --- | --- |
| `↵ / space` | View stash |
| `p / d` | Pop / drop selected stash |
| `/` | Filter stashes |

### Branches

| Key | Action |
| --- | --- |
| `↵` | Switch to the selected branch |
| `n / W` | New branch from the selection / new worktree for it |
| `d / D` | Delete branch / force delete |
| `R` | Include remote branches |
| `r` | Refresh |
| `y` | Copy branch name |

### Worktrees

| Key | Action |
| --- | --- |
| `↵` | Switch this pane to the worktree |
| `n` | New worktree (path and branch) |
| `d / D` | Remove worktree / force remove |
| `p` | Prune stale worktrees |
| `y` | Copy path |

### Hotspots

| Key | Action |
| --- | --- |
| `tab` | Cycle tree, files, authors |
| `h / l` | Fold a directory / expand the rest |
| `↵` | Open file history, fold a directory, or filter by author |
| `L` | Open history for the selected path |
| `o` | Open the file in `$EDITOR` |
| `V` | Review the selection as a commit range |
| `w` | Cycle 3 months, 12 months, all time |
| `a` | Show or hide generated and vendored files |
| `/` | Filter hotspots |

### Commit input

| Key | Action |
| --- | --- |
| `← / →, ctrl-b / ctrl-f, alt-b / alt-f` | Move by character or word |
| `home / end, ctrl-a / ctrl-e` | Start / end of subject |
| `ctrl-w / alt-backspace, ctrl-u / ctrl-k` | Delete previous word, to start, or to end |
| `backspace / delete, ctrl-d` | Delete character |
| `ctrl-g` | Write message in external editor |
| `↵ / esc` | Commit / cancel |

### Motion

| Key | Action |
| --- | --- |
| `j / k` | Down / up |
| `gg / G` | First / last |
| `ctrl-d / ctrl-u` | Half page down / up |
| `ctrl-f / ctrl-b` | Page down / up |
| `ctrl-e / ctrl-y` | Scroll the diff viewport |
| `} / {` | Next / previous group |
| `J / K` | Next / previous file |
| `) / (` | Next / previous commit in history or a range |
| `]c / [c` | Next / previous change |
| `gH / gM / gL` | Top / middle / bottom of screen |
| `zt / zz / zb` | Align the active line |
| `zh / zl` | Scroll horizontally |
| `ctrl-o / gi` | Jump back / forward |
| `count + motion` | Repeat a motion; with `gg` / `G`, jump to a source line |

### Finding

| Key | Action |
| --- | --- |
| `f` | File tree (`h`/`l` fold, `v` hides viewed files) |
| `/ / g?` | Search the diff forward / backward, or filter a list |
| `n / N` | Next / previous match |
| `L` | Open history for the current file |
| `gb` | Blame the current context or removed line |
| `← / →, home / end` | Move within a query |
| `alt-b / alt-f, ctrl-a / ctrl-e` | Move by word or to start / end |
| `ctrl-w / alt-backspace` | Delete the previous word |
| `backspace / delete` | Delete a character |
| `esc` | Clear the filter first, then go back |

### Acting

| Key | Action |
| --- | --- |
| `↵` | Open or activate the selection |
| `y / Y` | Copy the name or path / a location or link |
| `o` | Open the file in `$EDITOR` |
| `v` | Mark the file viewed / unviewed |
| `e` | Expand / collapse a deleted file |
| `h / l` | Fold / unfold the cursor: a file, commit body, or context |

### Toggles

| Key | Action |
| --- | --- |
| `w` | Toggle line wrapping |
| `W` | Toggle ignoring whitespace |
| `~` | Toggle short file paths |
| `T` | Toggle light / dark colors |

### General

| Key | Action |
| --- | --- |
| `?` | Show shortcuts |
| `esc / q` | Go back one screen; `q` quits on the first screen |
| `e` | Customize keys (in this dialog) |

### Customizing keys

The tables above are the defaults. Press `?` then `e` to open the key editor:
`↵` replaces an action's key with the next key you press, `a` adds another
key, `d` removes one of its keys, `x` resets the action, and `/` filters. Arrow keys, home / end and page
up / down work alongside the vim keys from the start.

Changes are saved to `~/.config/pane/keys` (`$XDG_CONFIG_HOME/pane/keys` when
set), which you can also edit by hand. It only lists the actions you changed:

```
# action = key, key; separate the keys of a sequence with spaces
search = ctrl+f, /
page-down = pgdown
next-file = alt+down
changes.push = ctrl+p
```

Shared actions such as `down` or `wrap` apply on every screen. Actions
prefixed with a screen, such as `changes.push` or `branches.delete`, apply
only there and take precedence over shared ones. Binding a key that another
action in the same scope uses offers to swap them. The key editor lists every
action ID. Keys use bubbletea names: `ctrl+x`, `alt+x`, `shift+tab`, `f3`,
`pgdown`, `space`. `ctrl+c` always interrupts, and text inputs such as the
commit message keep their editing keys. Terminals do not pass Cmd shortcuts
through to pane.

## Diffs and review

Every diff view numbers rows relative to the cursor; the cursor row shows its
source line, and `42gg` jumps to a source line. The active line is marked with
`▸`. The viewport keeps surrounding lines visible as you navigate and pins a
file header when its diff scrolls beyond the viewport.

Search is incremental and case-insensitive unless the query contains an
uppercase letter; matches are highlighted and `n` / `N` wrap around. The diff
jump list tracks searches and larger jumps within the current view. `gi` is
used for jump-forward because terminals commonly send `ctrl-i` as Tab, which
switches staged views. Paragraph boundaries are blank lines, file headers, and
hunk separators.

Press `W` to ignore whitespace differences in working-tree, commit, and
multi-commit diffs. The setting lasts for the session. Hunk staging and
discarding are disabled while it is on, because the displayed patch may omit
changes; whole-file staging remains available.

Lock files and generated files (`*.lock`, `*.snap`, `*.min.js`, `go.sum`,
`Cargo.lock`, `bun.lock`, `package-lock.json`, and paths marked
`linguist-generated` or `-diff` in `.gitattributes`) start collapsed and show
`generated` with line counts on the header. Press `l` to open one; the choice
lasts for the session.

Removed and added runs of three or more lines that reappear elsewhere in the
same commit render dim, with a `moved from path:line` hint after the addition.

Detected renames show the old and new paths on separate lines in staged
changes and commit views, with removed path text highlighted in red and added
text in green. Press `~` to shorten displayed paths to the parent directory and
filename; an ellipsis indicates omitted directories and is highlighted when
they differ. The file picker displays the new path and can search either full
name. Unstaged moves appear as a deletion and an untracked file, as in Git
status.

Submodule changes show the old and new revisions, available commit subjects,
and dirty-state notices. The file picker labels submodules. In multi-commit
review, search shows how many commits have loaded and includes new matches as
loading continues.

## Changes, commits, and staging

`space` stages or unstages the current whole file; `s` stages or unstages the
current hunk, or the whole file on its header. Untracked and deleted files are
staged or unstaged as whole files.

`d` discards the Git hunk containing the selected line, or all unstaged changes
in the selected file when on its header. Discarding an untracked file deletes
it from disk. Pane asks for confirmation; staged changes must be unstaged
first.

Commits include only staged changes if any are staged. Otherwise, pane stages
all non-ignored changes, including untracked files, before committing. Enter
commits the subject and `esc` cancels; `ctrl-c` quits. The subject edits like
pane's other text inputs. Pasting a multiline message puts the first line in the
editable subject and preserves the remaining lines as the body. If the commit
fails, the automatically staged changes remain staged. After a successful
commit, pane opens history with the new commit selected.

Press `ctrl-g` in the commit dialog to edit the full message in `$VISUAL` or
`$EDITOR`. Saving and closing the editor submits the commit; lines beginning
with `#` are ignored, and an empty message aborts. This also works when editing
the latest commit's message from history, subject to the same remote checks.

In history, `A` edits the latest commit's subject without changing its body,
contents, or staged changes; use `ctrl-g` to edit the body as well. Before
rewriting, pane checks the current branch and tag tips on every configured
remote. It refuses if the commit is reachable from one, if a remote cannot be
contacted, or if a remote's history is not available locally; fetch the
remote's branches and tags and try again in the latter case. With no configured
remotes, pane permits the edit. Remote state can still change between the check
and the rewrite.

`p` pushes the current branch. When the branch has no upstream, pane asks for
confirmation and runs `git push --set-upstream <remote> <branch>`; the remote
comes from `branch.<name>.pushRemote`, then `remote.pushDefault`, then
`origin`, then the only configured remote. With several remotes and no
configured choice, the dialog lists them by number. Press `a` to also set
`push.autoSetupRemote` in your global Git configuration; pane never changes
that setting unless you ask.

When a push is rejected because the remote moved, pane fetches the remote
branch and checks the local branch's reflog for its tip. If the branch once had
that commit, the rejection comes from rewriting history, as an amend or rebase
does, and pane lists the remote commits a force push would replace. Press `f`
to run `git push --force-with-lease`, pinned to the remote tip you were shown,
so it stops if anyone pushed since. Otherwise someone else pushed: pane lists
their commits and Enter pulls, then pushes.

`P` pulls the current branch. Pane offers a fix for these failures instead of
only showing Git's error:

- The upstream was deleted on the remote, as after a merged pull request: switch
  to the default branch (the remote's `HEAD`, then `main`, then `master`) and
  pull it. The old branch is kept.
- The branch has no upstream but the remote has a branch with the same name:
  track it and pull.
- The branch and its upstream have diverged and `pull.rebase` is not set: `m`
  merges and `r` rebases. `M` or `R` also sets `pull.rebase` in your global Git
  configuration; pane never changes that setting unless you ask.

When Git refuses a switch, pull, or merge because of uncommitted changes, Enter
on the error opens the changes view so you can stash or commit them.

## Commits, details, and blame

A commit and each commit in a multi-commit review start with a details block:
author and date, the committer when different, parents (marked `merge` when
there are several), refs pointing at the commit, and the message body with
trailers dimmed. Search covers it, `J` moves from it to the first file, and
`h` / `l` collapse, cap, or expand it for the session. Messages are limited to
ten body rows so long squash-merge descriptions do not push the diff away.

In a commit, `gb` on a context or removed line blames it against the first
parent and opens the commit that last touched it, selecting the file and line.
`ctrl-o` returns to where you were. On an added line there is nothing to blame
in the parent, so `gb` instead names the author and age of the commit on screen.

`Y` copies the new-side `path:line` in working-tree and base diffs; on a deleted
line it uses the nearest new-side line. In commit and multi-commit diffs, it
instead copies a permalink when the origin is a recognized GitHub or GitLab
remote, falling back to `path:line`. File history copies a link to the
historical path when available; ordinary history copies the selected hash.
File headers copy a path without a line number.

Copying uses `pbcopy`, `wl-copy`, `xclip`, or `xsel`. `o` opens the working-tree
file, so in a historical commit the line may have moved since.

## Viewed marks

Press `v` on any line of a file in a commit, range, or base review to mark it
viewed. Viewed files collapse and the cursor moves to the next unviewed file;
`v` again reopens and unmarks it. The file picker shows `✓` and `12/40 viewed`,
and `v` there hides viewed files. Marks last across launches in
`~/Library/Caches/pane` and are tied to the file's displayed change, so editing
a file after marking it viewed clears the mark, which suits reviewing an
agent's work in progress. Sequence reviews key marks by their first and last
commit.

## Context and editor hand-off

On a file header, `h` collapses the file and `l` reopens its standard diff. On
a diff line or `···`, `l` reveals nearby unchanged lines in chunks; repeat to
see more, or use `h` to undo a nearby expansion. This also works for staged
changes and historical commits.

`o` opens the current file at the active line in `$VISUAL` / `$EDITOR`.

## Stash

Press `S` in the changes view to stash the whole working tree, including
untracked (but not ignored) files. In history, `S` opens the stash list. Select
a stash and press Enter to inspect it, `p` to pop it, or `d` to drop it, and
`/` filters the list. Pane asks for confirmation before popping or dropping;
resolve any pop conflicts outside pane.

## Branches and worktrees

Press `b` in history to list branches with their upstream state, last commit,
and subject; branches checked out in another worktree are marked `⌂`. Enter
switches, `n` creates a branch from the selection, `W` creates a worktree for
it, `d` deletes with confirmation, `R` includes remote branches, `y` copies the
name, and `/` filters. Deleting the checked-out branch first switches to the
default branch, and only when tracked files have no uncommitted changes. When
`git branch -d` refuses an unmerged branch, as it does after a squash merge,
pane says how many commits are not in the checked-out branch and offers `-D`.

Press `tab` for worktrees, which show dirty counts and last activity, sorted
most recent first; Enter moves this pane into a worktree, `n` creates one using
the shared parent directory and the branch name, `d` removes it, and `p` prunes
stale entries. Switching worktrees keeps theme and wrapping but starts the new
repository's history and caches fresh. History marks commits that are the HEAD
of another worktree with `⌂`.

## History

In history, `j` / `k` select commits, `}` / `{` jump five commits, and `/`
searches author name or email, message, and hash. Enter or Space opens the
selected commit's read-only diff. Press `R` to review from the selected commit
to the working tree, or `V` to start a range selection, move with `j` / `k`,
and Enter to review the selected commits oldest first. While typing a search,
use the arrow keys to select a result and Enter to open it. Press `Esc` or `q`
to go back. If you opened a file from a diff's file picker, back returns to the
picker first; back again returns to history. Addition and deletion counts omit
binary files, which are counted separately. Merge commit counts and diffs are
relative to their first parent.

While editing a diff or history search, `ctrl-b` / `ctrl-f` move by character,
`ctrl-d` deletes the next character, and `ctrl-u` / `ctrl-k` delete to the start
/ end. In the file picker those keys retain their list-navigation behavior
while filtering; use the arrow keys to move the query cursor and the other
editing shortcuts above.

The history landing page shows the branch, local changes, and commits ahead of
or behind its configured upstream. Ahead/behind counts use locally stored
remote-tracking refs and may be stale; pane does not fetch automatically. Press
`F` to fetch and prune the current branch's upstream remote, or all remotes if
it has no upstream. History reloads when the checked-out commit changes. Push
and pull are available from history as well as the diff view. After a
successful push, pane returns to history and refreshes its status.

Lists share one grammar: `j` / `k`, `gg` / `G`, counts, half and full pages,
`{` / `}` groups, `/` filters, and `esc` clears a filter before leaving.

## Performance and development

`just b` builds pane, installs it, and runs it in the current repository.
`just release` and `just install` produce an optimized Apple Silicon build.
Both use profile-guided optimization from `default.pgo`. To refine it, run
`just profile`, use pane normally, quit, then run `just pgo` to merge the
session into the profile.

`just check` runs formatting, vetting, the build, and the tests. Performance
work uses the harness in [perf/README.md](../perf/README.md): `just perf`,
`just perf-external`, `just perf-diff`, and `just perf-check`.

## License

Pane is licensed under the [MIT License](../LICENSE).
