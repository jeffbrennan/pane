# Optimized builds target Apple Silicon (M1 and later implement ARMv8.4) and
# use default.pgo for profile-guided optimization when it exists.
release_env := "GOARM64=v8.4"
release_flags := '-trimpath -ldflags="-s -w"'

b:
    go build -o pane .
    go install .
    ./pane

release:
    {{release_env}} go build {{release_flags}} -o pane .

install:
    {{release_env}} go install {{release_flags}} .

# Use pane normally, quit, then run `just pgo` to fold the session into default.pgo.
profile:
    PANE_CPUPROFILE=.scratch/session.pprof ./pane

pgo:
    if [ -f default.pgo ]; then go tool pprof -proto default.pgo .scratch/session.pprof > .scratch/merged.pprof && mv .scratch/merged.pprof default.pgo; else cp .scratch/session.pprof default.pgo; fi

# Record docs/demo.gif with vhs from a Ghostty clone that has one unstaged edit,
# then add zooms. Needs vhs, uv, gifsicle and the Obel Mono Masked font.
demo:
    #!/usr/bin/env bash
    set -eu
    go build -o pane .
    export DEMO_REPO=.scratch/ghostty
    if [ -d "$DEMO_REPO" ]; then
        git -C "$DEMO_REPO" fetch -q origin
        git -C "$DEMO_REPO" reset -q --hard origin/HEAD
        git -C "$DEMO_REPO" clean -qfd
    else
        git clone -q https://github.com/ghostty-org/ghostty.git "$DEMO_REPO"
    fi
    sed -i '' 's/generally much faster?\./generally much faster./' "$DEMO_REPO/src/fastmem.zig"
    if git -C "$DEMO_REPO" diff --quiet; then echo "demo edit no longer applies; update the sed in the demo recipe" >&2; exit 1; fi
    export DEMO_REPO="$PWD/$DEMO_REPO"
    rm -rf .scratch/demo-frames
    vhs docs/demo.tape
    uv run docs/demo_zoom.py docs/demo.tape .scratch/demo-frames .scratch/demo-raw.gif
    gifsicle -O3 --lossy=20 .scratch/demo-raw.gif -o .scratch/demo.gif
    size=$(wc -c < .scratch/demo.gif)
    if [ "$size" -ge 10000000 ]; then echo "demo is $size bytes, over GitHub's 10 MB limit; raise --lossy" >&2; exit 1; fi
    mv .scratch/demo.gif docs/demo.gif

fmt:
    gofmt -w .

lint:
    go vet ./...

typecheck:
    go build ./...

test:
    go test ./...

check: fmt lint typecheck test

perf:
    #!/usr/bin/env bash
    set -eu
    mkdir -p .scratch
    export PANE_PERF_RUN="$(date -u +%Y%m%dT%H%M%S)-$$"
    PANE_PERF=1 PANE_PERF_LOG="$PWD/.scratch/perf-log.tsv" PANE_PERF_PROFILES="$PWD/.scratch/profiles" go test -run '^TestPerf$' -count=1 -v ./internal/ui
    go build -o .scratch/perf-pane .
    go run ./perf/firstpaint -repo "${PANE_PERF_REPO:-.}" -revision "${PANE_PERF_COMMIT:-HEAD}" -idle "${PANE_PERF_IDLE:-60s}" -content "${PANE_PERF_CONTENT:-}" -key "${PANE_PERF_KEY:-}" -after "${PANE_PERF_AFTER:-}" -wait "${PANE_PERF_WAIT:-read-only}" -log .scratch/perf-log.tsv

perf-external:
    #!/usr/bin/env bash
    set -eu
    mkdir -p .scratch .scratch/profiles
    export PANE_PERF_RUN="$(date -u +%Y%m%dT%H%M%S)-$$"
    go build -o .scratch/perf-pane .
    while IFS=$'\t' read -r name repo revision extra wait content; do
        case "$name" in ''|'#'*) continue ;; esac
        if [ ! -d "$repo" ]; then
            echo "skip $name: $repo is not available"
            continue
        fi
        PANE_PERF=1 PANE_PERF_SCENARIO="$name" PANE_PERF_REPO="$repo" PANE_PERF_COMMIT="$revision" PANE_PERF_LOG="$PWD/.scratch/perf-log.tsv" PANE_PERF_PROFILES="$PWD/.scratch/profiles" go test -run '^TestPerf$' -count=1 -v ./internal/ui
        content="${content:-${PANE_PERF_CONTENT:-}}"
        if [ -n "${extra:-}" ]; then
            go run ./perf/firstpaint -scenario "$name" -repo "$repo" -args "$extra" -idle "${PANE_PERF_IDLE:-60s}" -key "${PANE_PERF_KEY:-}" -after "${PANE_PERF_AFTER:-}" -content "$content" -wait "${wait:-read-only}" -log .scratch/perf-log.tsv
        else
            go run ./perf/firstpaint -scenario "$name" -repo "$repo" -revision "$revision" -idle "${PANE_PERF_IDLE:-60s}" -key "${PANE_PERF_KEY:-}" -after "${PANE_PERF_AFTER:-}" -content "$content" -wait "${wait:-read-only}" -log .scratch/perf-log.tsv
        fi
    done < perf/scenarios.txt

perf-diff:
    go run ./perf/diff .scratch/perf-log.tsv

# Set PANE_PERF_WORKTREES to a comma-separated list or a directory of worktrees.
perf-multi:
    #!/usr/bin/env bash
    set -eu
    go build -o .scratch/perf-pane .
    go run ./perf/firstpaint -binary .scratch/perf-pane -instances "${PANE_PERF_INSTANCES:-3}" -worktrees "${PANE_PERF_WORKTREES:?set PANE_PERF_WORKTREES}" -idle "${PANE_PERF_IDLE:-60s}" -log .scratch/perf-log.tsv

# Measure RSS after opening a huge commit and returning to history. Run once
# with PANE_KEEP_COMMIT_ROWS=1 to compare against retaining the rows.
perf-c4:
    #!/usr/bin/env bash
    set -eu
    go build -o .scratch/perf-pane .
    go run ./perf/firstpaint -binary .scratch/perf-pane -scenario c4-release -repo "${PANE_PERF_REPO:-.}" -revision "${PANE_PERF_COMMIT:-HEAD}" -key q -key-delay "${PANE_PERF_KEY_DELAY:-6s}" -settle 2s -idle "${PANE_PERF_IDLE:-6s}" -log .scratch/perf-log.tsv

# A dirty-tree scenario in a throwaway worktree, so the repo is not modified.
perf-dirty:
    #!/usr/bin/env bash
    set -eu
    repo="${PANE_PERF_REPO:-../bun}"
    files="${PANE_PERF_DIRTY_FILES:-300}"
    go build -o .scratch/perf-pane .
    worktree="$(mktemp -d "${TMPDIR:-/tmp}/pane-dirty.XXXXXX")"
    trap 'git -C "$repo" worktree remove --force "$worktree" >/dev/null 2>&1 || true' EXIT
    git -C "$repo" worktree add -q --detach "$worktree" HEAD
    git -C "$worktree" ls-files -z | tr '\0' '\n' | head -n "$files" | while IFS= read -r path; do
        printf '\n// perf dirty tree\n' >> "$worktree/$path"
    done
    go run ./perf/firstpaint -binary .scratch/perf-pane -scenario dirty-tree -repo "$worktree" -args - -wait "unstaged|staged|clean" -idle "${PANE_PERF_IDLE:-60s}" -log .scratch/perf-log.tsv

perf-check:
    #!/usr/bin/env bash
    set -eu
    mkdir -p .scratch
    export PANE_PERF_RUN="perf-check-$(date -u +%Y%m%dT%H%M%S)-$$"
    PANE_PERF=1 PANE_PERF_LOG="$PWD/.scratch/perf-check.tsv" go test -run '^TestPerf$' -count=1 ./internal/ui
    go run ./perf/check -baseline perf/baseline.tsv -log .scratch/perf-check.tsv

perf-baseline:
    #!/usr/bin/env bash
    set -eu
    mkdir -p .scratch
    rm -f .scratch/perf-baseline.tsv
    export PANE_PERF_RUN="baseline-$(date -u +%Y%m%dT%H%M%S)-$$"
    PANE_PERF=1 PANE_PERF_LOG="$PWD/.scratch/perf-baseline.tsv" go test -run '^TestPerf$' -count=1 ./internal/ui
    awk -F'\t' 'BEGIN { OFS = "\t"; print "# Synthetic performance baseline. Update with: just perf-baseline" } { print $3, $4, $5 }' .scratch/perf-baseline.tsv | sort -u > perf/baseline.tsv
