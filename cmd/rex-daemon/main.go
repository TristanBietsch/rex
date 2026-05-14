// Package main is the rex-daemon entry point.
package main

import (
	"fmt"
	"os"
)

const version = "0.0.1-plan-a"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rex-daemon:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fmt.Println("rex-daemon", version)
	_ = args
	return nil
}
