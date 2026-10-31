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
    gofmt -w *.go perf/firstpaint/*.go

lint:
    go vet ./...

typecheck:
    go build ./...

test:
    go test ./...

check: fmt lint typecheck test

perf:
    mkdir -p .scratch
    PANE_PERF=1 PANE_PERF_LOG=.scratch/perf-log.tsv PANE_PERF_PROFILES=.scratch/profiles go test -run '^TestPerf$' -count=1 -v .
    go build -o .scratch/perf-pane .
    go run ./perf/firstpaint -repo "${PANE_PERF_REPO:-.}" -revision "${PANE_PERF_COMMIT:-HEAD}" -idle "${PANE_PERF_IDLE:-60s}" -log .scratch/perf-log.tsv
