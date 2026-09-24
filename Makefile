# GOWORK=off: this repo must build standalone, not through the workspace go.work.
GO := GOWORK=off go

.PHONY: build test vet fmt check demo fixtures clean

build:
	$(GO) build ./...

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

check: build vet test
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed on:" >&2; echo "$$out" >&2; exit 1; fi

# Scan the bundled fixtures and write report.json / report.html to the repo root.
demo:
	$(GO) run ./cmd/distillscan scan fixtures

# Regenerate fixtures deterministically (fixed seed; running twice is a no-op diff).
fixtures:
	$(GO) run ./tools/genfixtures

clean:
	rm -f report.json report.html distillscan
