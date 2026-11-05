# Performance harness

Run the in-process, synthetic, terminal, and profile measurements with:

```sh
just perf
just perf-diff
```

`just perf` measures the large commit selected by `PANE_PERF_REPO` and
`PANE_PERF_COMMIT`, plus synthetic huge-file, long-line, many-small, refresh,
range, base, history, and syntax scenarios. Results are appended to
`.scratch/perf-log.tsv`; profiles are written to `.scratch/profiles`.

To run every available repository in `perf/scenarios.txt`:

```sh
just perf-external
```

Each line contains a tab-separated scenario name, repository path, revision,
and optional pane arguments. Missing repositories are skipped. Passing `-` as
the pane arguments opens pane with no arguments, which lands on history in a
clean tree.

To measure several worktree sessions at once:

```sh
PANE_PERF_WORKTREES="../wt1,../wt2,../wt3" just perf-multi
```

`PANE_PERF_WORKTREES` accepts a comma-separated list or a directory whose
subdirectories are worktrees. The run leaves one pane focused, sends focus-out
to the rest, and logs combined RSS, CPU, terminal bytes and the observed Git command rate per minute under the `multi` scenario.

`perf-diff` labels every row `noise`, `improved`, or `regressed` using plan
14's rule (5%, or 2 ms for durations under 40 ms). It omits `idle_cpu_ns` and
`idle_terminal_bytes` when the two runs observed idle time for different
lengths or for less than 30 s, and says so.

## Regression gate

```sh
just perf-check
just perf-baseline
```

`just perf-check` runs the synthetic scenarios and fails when any metric is
more than twice its value in `perf/baseline.tsv`. Durations also have to grow
by at least a millisecond, so microsecond noise cannot fail the gate.
`just perf-baseline` rewrites the checked-in baseline after an intentional
change.

## Current large-commit results

Measured on Apple Silicon (the same hardware behind `perf/baseline.tsv`)
against a 9,195-file, 1.5M-row commit, except the base review row, which uses a
36,008-file, 271 MB working-tree diff:

| Metric | Result | Initial target | Status |
|---|---|---|---|
| First diff content, commit | ~116 ms | < 150 ms | met |
| First diff content, base review | ~359 ms (was ~4.40 s) | < 150 ms | improved, above target |
| Peak RSS, base review | ~681 MB (was 1.99 GB) | — | improved |
| Full commit build | ~63 ms (was ~161 ms) | — | improved |
| Picker key / render | < 1 µs / ~1.4 ms | < 8 ms combined | met |
| Search miss | ~12.2 ms | < 16 ms | met |
| Width resize rebuild | ~42 ms (was ~146–189 ms) | < 50 ms | met |
| Height-only resize | < 1 ms | 0 | met |
| Heap after build | ~249 MB | < 200 MB | not met; revised to 250 MB |
| Idle CPU, 60 s commit view | ~0.41 s (0.68%) | < 0.30 s (0.5%) | not met; revised to 1% |
| Idle terminal output | < 100 B/min | minimal | met |
| Hotspot first rows, 1-year bun window | ~50 ms | < 200 ms | met |
| Hotspot cache-hit launch | ~60 ms | < 100 ms | met (summary incl. prebuilt tree) |
| Hotspot full one-year window | ~5.7 s | a few seconds | improved from 14.3 s; 24 staggered parallel git log walks, Git-bound |

The remaining heap is predominantly row text and the loaded patch itself;
further reductions require changing how diff text is retained. Width resize
splices only the lines whose wrapping changes, so it touches roughly 8% of the
rows in files that need any rewrap; the rest are moved without being rebuilt.
Rows carry a small capacity margin so neighbouring widths reuse the existing
backing array instead of allocating a new one.

Base review uses the same stream as commits, so it paints after its first
screenful instead of after the whole diff. Its 359 ms is dominated by the
`--name-status` pass over 36,008 paths (~180 ms) before the first chunk can be
matched to paths; the patch bytes are ready much earlier.

`just perf-c4` opens a huge commit, presses `q` after `-key-delay`, and records `footprint_after_idle_bytes` and `rss_after_idle_bytes`; set `PANE_KEEP_COMMIT_ROWS=1` on a second run to compare against retaining the rows. `footprint_after_idle_bytes` uses macOS `vmmap`, which excludes Go pages marked reusable and tracks what Activity Monitor reports; raw `ps` RSS still counts those pages. `-key-delay` waits before sending a key, and `-dump <path>` writes the pane's terminal output for inspecting streamed screens. `PANE_MOVED=0` disables lazy moved-block detection, and `moved_index_ns` records its index-building cost.

`resize_width_apply_ns` measures the debounced rebuild that follows
`tea.WindowSizeMsg`, so it reflects the work a user waits for after resizing.

`scroll_j_mid_ns`, `scroll_j_end_ns`, and `scroll_ctrl_d_mid_ns` time one frame
of a scroll in the middle and at the end of the loaded diff, and
`view_alloc_bytes` records what one `View()` allocates. Set
`PANE_PERF_SCROLL_FRAMES` to repeat scrolling and rendering enough times for
the CPU profile to show the render path.

## Mega-commit scroll result

Measured on bun `23427dbc12f` ("Rewrite Bun in Rust"): 2,188 files and
1,017,161 rows.

| Metric | Before | After |
|---|---|---|
| `scroll_j_mid_ns` | 2.42 ms | 36 µs |
| `scroll_ctrl_d_mid_ns` | 2.26 ms | 34 µs |
| `scroll_j_end_ns` | 2.46 ms | 39 µs |
| `view_ns` | 150 µs | 156 µs |

`commitModel` recomputed the viewed badge by scanning every row, and each key
press built the commit model three times: twice while checking header keys and
once more for the in-diff handler. The scan moved to the points where rows are
built or appended, so a scroll frame now walks only the visible rows. `View()`
itself was already proportional to the viewport; the remaining cost is inline
diff spans for visible lines.

## Linux results

The numbers below are from an Intel Core i5-8250U (4C/8T, 2017 ULV) running
Linux and Go 1.27.3 on 2026-11-05. The Apple column is `perf/baseline.tsv`; it
will usually stay faster because it is newer silicon. Compare the Linux
before/after columns when judging a change.

The before column is commit `17502e6` measured in a clean worktree. The now
column is the code at `4fe1ae1`. Timings are medians of six runs for the
harness metrics, and each run is already the harness's median of five. This
ULV laptop shows noticeable run-to-run variance under sustained load, so treat
differences under 5% or 2 ms as noise, matching `perf-diff`'s rule.

| Metric | Apple baseline | Linux before | Linux now | Change |
|---|---:|---:|---:|---:|
| Heap after build, 51k rows | 8.30 MB | 8.21 MB | 7.15 MB | −13% |
| `row` struct size | 104 B | 104 B | 80 B | −23% |
| `view_alloc_bytes`, 51k rows | 299 KB | 299 KB | 25.8 KB | −91% |
| `view_ns`, 51k rows | 207 µs | 228 µs | 68 µs | −70% |
| `view_ns`, 100k-row file | 170 µs | 238 µs | 73 µs | −69% |
| `moved_index_ns`, 51k rows | 3.49 ms | 4.95 ms | 2.59 ms | −48% |
| `moved_index_ns`, 100k-line file | 6.43 ms | 11.26 ms | 4.18 ms | −63% |
| `synthetic-range rebuild_ns`, 100 commits | 2.07 ms | 6.81 ms | 1.55 ms | −77% |
| `synthetic-refresh full_rebuild_ns`, 1000 files | 3.19 ms | 11.40 ms | 8.14 ms | −29% |
| `synthetic-refresh spliced_refresh_ns`, 1000 files | 2.99 ms | 7.30 ms | 5.12 ms | −30% |

Paths that were not the target of this pass are broadly unchanged:

| Metric | Apple baseline | Linux before | Linux now |
|---|---:|---:|---:|
| `search_miss_ns`, 51k rows | 1.60 ms | 3.06 ms | 3.31 ms (noise) |
| `resize_width_apply_ns` | 0.77 ms | 2.34 ms | 1.77 ms |
| `wrap_toggle_ns` | 1.63 ms | 3.06 ms | 3.33 ms (noise) |
| `scroll_j_mid_ns` | 20.7 µs | 27.8 µs | 26.8 µs (noise) |

The full harness's `synthetic-many build_ns` is noisy on this machine
(5.4–7.2 ms across runs). The isolated `BenchmarkRebuild` is the stable
measure, and a same-session worktree A/B showed no rebuild regression.

### Render and rebuild benchmarks

These direct benchmarks cover the shapes the recent UI work targets:
all-added rows, paired removed/added rows with inline word diffs, and a
one-row scroll. `BenchmarkRenderView/additions` started this pass at roughly
265 µs, 299 KB, and 3,943 allocations per frame.

| Benchmark | Result |
|---|---:|
| `BenchmarkRenderView/additions` | 70 µs, 26 KB, 306 allocs |
| `BenchmarkRenderView/mixed` | 108 µs, 37 KB, 395 allocs |
| `BenchmarkRenderScroll/additions` | 102 µs, 55 KB, 321 allocs |
| `BenchmarkRenderScroll/mixed` | 143 µs, 66 KB, 410 allocs |
| `BenchmarkRebuild/many` | 4.58 ms, 4.95 MB |
| `BenchmarkRebuild/huge` | 13.0 ms, 8.01 MB |
| `BenchmarkCollapsedGenerated/expanded` | 10.1 ms, 8.01 MB |
| `BenchmarkCollapsedGenerated/collapsed` | 2.14 ms, 1.6 KB |

```sh
go test ./internal/ui -run '^$' -bench 'BenchmarkRender|BenchmarkRebuild|BenchmarkCollapsed' -benchmem
```

`just perf-check` reports several metrics above twice the Apple baseline on
this machine. Those are hardware-bound and are not Linux regressions; use the
same-machine before/after columns or a Linux baseline when adding a new gate.

To add a harness row, run the synthetic harness and append the new medians:

```sh
PANE_PERF=1 PANE_PERF_RUN=linux-$(date -u +%Y%m%dT%H%M%S) \
  PANE_PERF_LOG="$PWD/.scratch/perf-linux.tsv" \
  go test -run '^TestPerf$' -count=1 ./internal/ui
```
