package main

import (
	"fmt"
	"os"

	"github.com/fruh/idb/cmd"
)

func main() {
	if err := cmd.Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "idb:", err)
		if h := cmd.ErrorHint(); h != "" {
			fmt.Fprintln(os.Stderr, "hint:", h)
		}
		os.Exit(1)
	}
}
