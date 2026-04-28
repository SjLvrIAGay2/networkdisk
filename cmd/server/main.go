package main

import (
	"fmt"
	"os"

	"networkdisk/internal/app"
)

func main() {
	configPath := "config.toml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}
	if err := app.Run(configPath); err != nil {
		_, err := fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		if err != nil {
			return
		}
		os.Exit(1)
	}
}
