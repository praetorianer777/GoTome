package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: genres gutenberg|library|eval ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "gutenberg":
		err = extractGutenberg(os.Args[2:])
	case "library":
		err = extractLibrary(os.Args[2:])
	case "shipped":
		err = runShipped(os.Args[2:])
	case "stats":
		err = runStats(os.Args[2:])
	case "port":
		err = runPort(os.Args[2:])
	case "eval":
		err = runEval(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
