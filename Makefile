BINARY       := terraform-provider-devhelm
INSTALL_PATH := ~/.terraform.d/plugins/registry.terraform.io/devhelmhq/devhelm/0.1.0-dev/$(shell go env GOOS)_$(shell go env GOARCH)

.PHONY: build install test testacc lint fmt typegen generate docs clean

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

generate: typegen
	go generate ./...

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate

clean:
	rm -f $(BINARY)

release:
	./scripts/release.sh $(VERSION)
