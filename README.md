# pane

A narrow, keyboard-first terminal viewer for Git changes. Diffs show file
headers, line numbers, and added and removed lines without Git's patch
metadata. Run `pane` anywhere inside a Git working tree.

```
 1  internal/ui/view.go
 2  @@ -14,7 +14,7 @@
 3     func (m model) View() string {
 4 -       return m.render()
 5 +       return m.render(width)
 6     }
```

## Features

- **Changes:** unstaged and staged diffs with hunk staging, discarding, committing, push and pull.
- **History:** commit list with search, push/pull, fetching, message edits, and stash access.
- **Commit and range review:** read-only diffs with next/previous commit jumps and oldest-first ranges.
- **Base review:** one review from a merge base to the working tree, including staged, unstaged, and untracked changes.
- **File history:** follow one path across renames and step through the commits that touched it.
- **Blame:** open the commit that last touched the line under the cursor.
- **Branches and worktrees:** switch, create, delete, copy, filter, and move this pane into a worktree.
- **Stash:** push, list, inspect, pop, and drop stashes.
- **Hotspots:** explore an unfamiliar repository by attention-weighted activity, by tree, file, or author.
- **Review state:** line wrapping, whitespace toggling, short paths, viewed marks, search, and an editor hand-off.

## Install

```sh
go install github.com/jeffbrennan/pane@latest
```

Go and Git must be installed. The initial target is macOS.

## Quickstart

```sh
pane                      # working-tree changes, or history when clean
pane HEAD~5..HEAD         # review a commit range
pane hot                  # explore activity in an unfamiliar repository
```

## Learn more

See [docs/guide.md](docs/guide.md) for every key, the CLI forms, hotspot
scoring, caching, and development notes.

## License

Pane is licensed under the [MIT License](LICENSE).
