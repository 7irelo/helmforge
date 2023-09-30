package main

import (
	"os"

	"github.com/7irelo/helmforge/internal/cli/commands"
)

func main() {
	os.Exit(commands.Execute())
}
