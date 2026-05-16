.PHONY: check tidy lint test build

# CI gate: everything that must pass before merge.
check: tidy lint test

# Tidy and verify the module is clean. Catches both modified tracked and
# newly-generated untracked go.mod/go.sum (the latter once deps are added).
tidy:
	go mod tidy
	@test -z "$$(git status --porcelain -- go.mod go.sum)" || \
		{ echo "go.mod/go.sum not tidy:"; git status --porcelain -- go.mod go.sum; git diff -- go.mod go.sum; exit 1; }

lint:
	golangci-lint run

test:
	go test -race ./...

build:
	go build ./...
