.PHONY: check tidy lint test build

# CI gate: everything that must pass before merge.
check: tidy lint test

# Tidy and verify the module is clean. Stdlib-only module: no go.sum, so the
# pathspec form tolerates its absence (a no-op on an untracked path).
tidy:
	go mod tidy
	git diff --exit-code -- go.mod go.sum

lint:
	golangci-lint run

test:
	go test -race ./...

build:
	go build ./...
