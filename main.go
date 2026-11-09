// Command terramate-atlantis-config generates atlantis.yaml from Terramate
// stacks.
package main

import (
	"os"

	"github.com/devops-roast/terramate-atlantis-config/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
