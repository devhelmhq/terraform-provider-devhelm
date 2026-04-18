//go:build tools

// Package tools tracks build-time-only dependencies via blank imports so
// that `go mod tidy` keeps them pinned in go.mod / go.sum without requiring
// them at runtime. Idiomatic Go convention; see
// https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module.
package tools

import (
	// tfplugindocs — generates the docs/ tree consumed by the Terraform
	// Registry from schema descriptions and the examples/ directory.
	// Invoked via `go generate ./...` (see internal/provider/generate.go)
	// and `make docs`.
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
