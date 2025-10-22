package cmd

import (
	"fmt"
	"os"

	"github.com/fabio/go-magento-cron-monitor/internal/config"
	"github.com/fabio/go-magento-cron-monitor/internal/database"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test database connection",
	Long:  `Test the database connection using the configuration file.`,
	Run:   runTest,
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) {
	// Load configuration
	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Testing database connection to %s:%d/%s...\n", 
		cfg.Database.Host, cfg.Database.Port, cfg.Database.Name)

	// Create database client
	db, err := database.NewClient(cfg.Database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Test the connection
	if err := db.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "Connection test failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Database connection successful!")

	// Try to query cron_schedule table
	count, err := db.GetCronScheduleCount()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not query cron_schedule table: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Found %d records in cron_schedule table\n", count)
	fmt.Println("\nDatabase test completed successfully!")
}
