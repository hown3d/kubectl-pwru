package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra/doc"

	"github.com/hown3d/kubectl-pwru/cmd/plugin/commands"
)

func main() {
	if len(os.Args) < 2 {
		panic("must provide target directory as argument")
	}
	dir := os.Args[1]

	if err := doc.GenMarkdownTree(commands.RootCmd(), dir); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
