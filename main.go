package main

import (
	"fmt"
	"os"

	"github.com/fabio/go-magento-cron-monitor/cmd"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Handle version command
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("go-magento-cron-monitor version %s\n", version)
		fmt.Printf("commit: %s\n", commit)
		fmt.Printf("built: %s\n", date)
		os.Exit(0)
	}

	cmd.Execute()
}
