package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tn3270proxy:", err)
		os.Exit(1)
	}
}

// run is the real entrypoint, kept separate from main for testability.
// Subcommands (serve, seed) are wired in later tasks.
func run(args []string) error {
	fmt.Println("tn3270proxy: not yet implemented")
	return nil
}
