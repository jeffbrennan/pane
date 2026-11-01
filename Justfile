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

fmt:
    gofmt -w *.go perf/firstpaint/*.go perf/diff/*.go

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
    PANE_PERF=1 PANE_PERF_LOG=.scratch/perf-log.tsv PANE_PERF_PROFILES=.scratch/profiles go test -run '^TestPerf$' -count=1 -v .
    go build -o .scratch/perf-pane .
    go run ./perf/firstpaint -repo "${PANE_PERF_REPO:-.}" -revision "${PANE_PERF_COMMIT:-HEAD}" -idle "${PANE_PERF_IDLE:-60s}" -content "${PANE_PERF_CONTENT:-}" -key "${PANE_PERF_KEY:-}" -after "${PANE_PERF_AFTER:-}" -log .scratch/perf-log.tsv

perf-external:
    #!/usr/bin/env bash
    set -eu
    mkdir -p .scratch .scratch/profiles
    export PANE_PERF_RUN="$(date -u +%Y%m%dT%H%M%S)-$$"
    go build -o .scratch/perf-pane .
    while IFS=$'\t' read -r name repo revision extra; do
        case "$name" in ''|'#'*) continue ;; esac
        if [ ! -d "$repo" ]; then
            echo "skip $name: $repo is not available"
            continue
        fi
        PANE_PERF=1 PANE_PERF_SCENARIO="$name" PANE_PERF_REPO="$repo" PANE_PERF_COMMIT="$revision" PANE_PERF_LOG=.scratch/perf-log.tsv PANE_PERF_PROFILES=.scratch/profiles go test -run '^TestPerf$' -count=1 -v .
        if [ -n "${extra:-}" ]; then
            go run ./perf/firstpaint -scenario "$name" -repo "$repo" -args "$extra" -idle "${PANE_PERF_IDLE:-60s}" -log .scratch/perf-log.tsv
        else
            go run ./perf/firstpaint -scenario "$name" -repo "$repo" -revision "$revision" -idle "${PANE_PERF_IDLE:-60s}" -log .scratch/perf-log.tsv
        fi
    done < perf/scenarios.txt

perf-diff:
    go run ./perf/diff .scratch/perf-log.tsv
