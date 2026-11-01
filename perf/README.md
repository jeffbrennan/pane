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

Measured against a 9,195-file, 1.5M-row commit, except the base review row,
which uses a 36,008-file, 271 MB working-tree diff:

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

`just perf-c4` opens a huge commit, presses `q` after `-key-delay`, and records `rss_after_idle_bytes`; set `PANE_KEEP_COMMIT_ROWS=1` on a second run to compare against retaining the rows. `-key-delay` delays a sent key so a run can wait for a huge commit to finish loading before pressing `q`. `rss_after_idle_bytes` samples the process's current RSS after the idle window, and `PANE_KEEP_COMMIT_ROWS=1` keeps a finished commit's rows so C4's release can be measured against it.

`resize_width_apply_ns` measures the debounced rebuild that follows
`tea.WindowSizeMsg`, so it reflects the work a user waits for after resizing.
