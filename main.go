package main

import (
	"fmt"
	"os"

	"github.com/GameTec-live/pockethost/internal/master"
	"github.com/GameTec-live/pockethost/internal/tenant"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "tenant" {
		if err := tenant.Run(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := master.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
