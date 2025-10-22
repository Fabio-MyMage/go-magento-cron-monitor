package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fabio/go-magento-cron-monitor/internal/config"
	"github.com/fabio/go-magento-cron-monitor/internal/database"
	"github.com/fabio/go-magento-cron-monitor/internal/logger"
	"github.com/fabio/go-magento-cron-monitor/internal/monitor"
	"github.com/fabio/go-magento-cron-monitor/internal/pidfile"
	"github.com/spf13/cobra"
)

var daemon bool

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Start monitoring Magento cron jobs",
	Long: `Start the cron monitor daemon that periodically checks the cron_schedule
table for stuck or problematic cron jobs and logs alerts.`,
	Run: runMonitor,
}

func init() {
	rootCmd.AddCommand(monitorCmd)
	monitorCmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "run in daemon mode")
}

func runMonitor(cmd *cobra.Command, args []string) {
	// Handle daemon mode
	if daemon {
		if err := runAsDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to daemonize: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Load configuration
	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Adjust log level based on verbosity
	if verbose >= 3 {
		cfg.Logging.Level = "debug"
	} else if verbose == 2 {
		cfg.Logging.Level = "info"
	}

	// Initialize logger
	log, err := logger.New(cfg.Logging, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Close()

	// Create and check PID file
	pidPath := pidfile.GetDefaultPath(cfgFile)
	pid := pidfile.New(pidPath)
	if err := pid.Create(); err != nil {
		log.Error("Failed to create PID file", err, nil)
		os.Exit(1)
	}
	defer pid.Remove()

	log.Info("Starting Magento Cron Monitor", map[string]interface{}{
		"host":     cfg.Database.Host,
		"database": cfg.Database.Name,
		"interval": cfg.Monitor.Interval.String(),
		"pidfile":  pidPath,
	})

	// Create database client
	db, err := database.NewClient(cfg.Database)
	if err != nil {
		log.Error("Failed to connect to database", err, nil)
		os.Exit(1)
	}
	defer db.Close()

	// Create monitor service
	svc := monitor.NewService(cfg, db, log, verbose)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start monitoring in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- svc.Start()
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		log.Info("Received shutdown signal", map[string]interface{}{"signal": sig.String()})
		svc.Stop()
		log.Info("Monitor stopped", nil)
	case err := <-errChan:
		if err != nil {
			log.Error("Monitor error", err, nil)
			os.Exit(1)
		}
	}
}

func runAsDaemon() error {
	// Build args without -d/--daemon flag
	args := []string{os.Args[0]}
	
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		
		// Skip -d and --daemon
		if arg == "-d" || arg == "--daemon" {
			continue
		}
		
		// Handle combined short flags like -dvvv
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "d") {
			// Remove 'd' from combined flags
			newArg := strings.ReplaceAll(arg, "d", "")
			if newArg != "-" {
				args = append(args, newArg)
			}
			continue
		}
		
		args = append(args, arg)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	fmt.Printf("Monitor started in background (PID: %d)\n", cmd.Process.Pid)
	fmt.Printf("To stop: kill %d\n", cmd.Process.Pid)
	
	cmd.Process.Release()
	return nil
}
