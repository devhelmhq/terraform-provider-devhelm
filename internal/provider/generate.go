package provider

// Run `go generate ./...` to regenerate the docs/ tree from the provider
// schema and the examples/ directory. The generated docs are what the
// Terraform Registry serves at registry.terraform.io/providers/devhelmhq/devhelm.
//
// Generation is also wired into `make docs` and verified in CI by
// `make docs-check`, which fails if the committed docs/ tree drifts from
// what the generator would produce.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name devhelm --rendered-provider-name devhelm
