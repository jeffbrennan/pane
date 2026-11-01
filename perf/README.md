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
| First diff content | ~119 ms | < 150 ms | met |
| Picker key / render | < 1 µs / ~6.3 ms | < 8 ms combined | met |
| Search miss | ~12.7 ms | < 16 ms | met |
| Width resize rebuild | ~159 ms | < 50 ms | not met |
| Height-only resize | < 1 ms | 0 | met |
| Heap after build | ~246 MB | < 200 MB | not met; revised to 250 MB |
| Idle CPU, 60 s commit view | ~0.44 s (0.73%) | < 0.30 s (0.5%) | not met; revised to 1% |
| Idle terminal output | < 100 B/min | minimal | met |

The remaining heap is predominantly row text and the loaded patch itself;
further reductions require changing how diff text is retained. Width resize
remains dominated by rebuilding wrapped rows.
