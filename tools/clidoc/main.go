// Command clidoc writes docs/cli.md from the CLI's declarations (run by `make spec`).
package main

import (
	"fmt"
	"os"

	"github.com/nerveband/townsquare/internal/cli"
)

func main() {
	md, err := cli.Markdown()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clidoc:", err)
		os.Exit(1)
	}
	if err := os.WriteFile("docs/cli.md", []byte(md), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "clidoc:", err)
		os.Exit(1)
	}
	fmt.Println("wrote docs/cli.md")
}
