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
and optional pane arguments. Missing repositories are skipped.

## Current large-commit results

Measured against a 9,195-file, 1.5M-row commit:

| Metric | Result | Initial target | Status |
|---|---|---|---|
| First diff content | ~116 ms | < 150 ms | met |
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

`resize_width_apply_ns` measures the debounced rebuild that follows
`tea.WindowSizeMsg`, so it reflects the work a user waits for after resizing.
