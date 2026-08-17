# GOWORK=off: this repo must build standalone, not through the workspace go.work.
GO := GOWORK=off go

.PHONY: build test vet check demo fixtures clean

build:
	$(GO) build ./...

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: build vet test

# Scan the bundled fixtures and write report.json / report.html to the repo root.
demo:
	$(GO) run ./cmd/distillscan scan fixtures

# Regenerate fixtures deterministically (fixed seed; running twice is a no-op diff).
fixtures:
	$(GO) run ./tools/genfixtures

clean:
	rm -f report.json report.html distillscan
