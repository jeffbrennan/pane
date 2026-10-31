b:
    go build -o pane .
    ./pane

fmt:
    gofmt -w *.go

lint:
    go vet ./...

typecheck:
    go build ./...

test:
    go test ./...

check: fmt lint typecheck test
