package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose int
)

var rootCmd = &cobra.Command{
	Use:   "go-magento-cron-monitor",
	Short: "Monitor Magento 2 cron jobs for stuck/problematic executions",
	Long: `A CLI tool to monitor Magento 2 cron jobs by querying the cron_schedule
database table, detecting stuck/problematic crons, and logging alerts to a file.

Supports multiple detection criteria:
  - Long-running jobs
  - Accumulating pending jobs
  - Consecutive errors
  - Missed executions
  - Stale running jobs`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "config.yaml", "config file path")
	rootCmd.PersistentFlags().CountVarP(&verbose, "verbose", "v", "verbosity level (-v, -vv, -vvv)")
}
