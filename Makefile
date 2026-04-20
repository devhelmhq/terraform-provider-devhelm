BINARY       := terraform-provider-devhelm
INSTALL_PATH := ~/.terraform.d/plugins/registry.terraform.io/devhelmhq/devhelm/0.1.0-dev/$(shell go env GOOS)_$(shell go env GOARCH)

.PHONY: build install test testacc lint fmt typegen generate docs docs-check clean

build:
	go build -o $(BINARY)

install: build
	mkdir -p $(INSTALL_PATH)
	cp $(BINARY) $(INSTALL_PATH)/

test:
	go test ./... -v -count=1

testacc:
	TF_ACC=1 go test ./internal/provider/... -v -count=1 -timeout 30m

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

typegen:
	./scripts/typegen.sh

generate: typegen docs

docs:
	./scripts/generate-docs.sh

# Used in CI: regenerate docs and confirm the committed tree matches.
# Fails if the docs are stale (e.g. someone edited a schema description
# without re-running `make docs`) or if newly generated files weren't
# committed. Scoped to tfplugindocs output paths so the unrelated
# docs/openapi/ source spec doesn't trip the check.
TFPLUGINDOCS_PATHS := docs/index.md docs/resources docs/data-sources docs/guides
docs-check:
	@./scripts/generate-docs.sh
	@status="$$(git status --porcelain -- $(TFPLUGINDOCS_PATHS))"; \
	if [ -n "$$status" ]; then \
		echo ""; \
		echo "ERROR: Generated docs are out of date. Re-run 'make docs' and commit."; \
		echo "$$status"; \
		git --no-pager diff --stat -- $(TFPLUGINDOCS_PATHS); \
		exit 1; \
	fi

clean:
	rm -f $(BINARY)

release:
	./scripts/release.sh $(VERSION)
