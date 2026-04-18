GO ?= go
DIST ?= dist
ARCH ?= amd64

.PHONY: agents-sync agents-verify clean-repo coverage deadcode fix integration mutation package-smoke public-verify release-archive-smoke release-repro test test-race verify vet

agents-sync:
	scripts/sync_agent_surfaces.sh

agents-verify:
	scripts/verify_agent_surfaces.sh

public-verify:
	scripts/verify_public_repo_surface.sh

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

verify: agents-verify public-verify test test-race vet

coverage:
	$(GO) test -cover ./...

fix:
	$(GO) fix ./...

deadcode:
	@tool="$$(command -v deadcode || true)"; \
	if [[ -z "$$tool" && -x "$$($(GO) env GOPATH)/bin/deadcode" ]]; then \
		tool="$$($(GO) env GOPATH)/bin/deadcode"; \
	fi; \
	if [[ -z "$$tool" ]]; then \
		echo "deadcode not found in PATH or GOPATH/bin" >&2; \
		exit 1; \
	fi; \
	output="$$("$$tool" -test ./...)"; \
	if [[ -n "$$output" ]]; then \
		printf '%s\n' "$$output"; \
		exit 1; \
	fi

integration:
	scripts/run_release_integration.sh

release-archive-smoke:
	scripts/release_archive_smoke.sh "$(DIST)"

release-repro:
	scripts/release_reproducibility.sh "$(DIST)"

package-smoke:
	scripts/release_linux_package_smoke.sh "$(DIST)" "$(ARCH)"

mutation:
	scripts/run_mutation_supported.sh

clean-repo:
	scripts/clean_repo_detritus.sh
