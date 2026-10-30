package main

import (
	"avatarharness/internal/cli"
	"fmt"
	"os"
)

func main() {
	if err := cli.Execute(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(2)
	}
}
