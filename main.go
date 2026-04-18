package main

import (
	"context"
	"log"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is overridden at release time by the linker (see .goreleaser.yml).
// The compiled-in default doubles as the "I'm running a local dev build"
// signal in user-agent headers and `terraform providers` output.
const version = "0.1.0-dev"

func main() {
	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/devhelmhq/devhelm",
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err)
	}
}
