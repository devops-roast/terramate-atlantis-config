package main

import (
	"os"

	"github.com/devops-roast/terramate-atlantis-config/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
