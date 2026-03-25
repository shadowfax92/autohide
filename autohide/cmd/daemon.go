package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"autohide/config"
	"autohide/daemon"
	"autohide/menubar"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var noMenubar bool

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the autohide daemon in the foreground",
	RunE:  runDaemon,
}

func init() {
	daemonCmd.Flags().BoolVar(&noMenubar, "no-menubar", false, "run without menu bar (headless mode)")
	rootCmd.AddCommand(daemonCmd)
}

func runDaemon(cmd *cobra.Command, args []string) error {
	cfgPath := configPath()
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	level := zerolog.InfoLevel
	if verbose {
		level = zerolog.DebugLevel
	}
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		Level(level).
		With().Timestamp().Logger()

	d := daemon.New(cfg, cfgPath, logger)

	sockPath := config.SocketPath()
	srv := daemon.NewServer(d, sockPath, logger)
	if err := srv.Start(); err != nil {
		return err
	}
	defer srv.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info().Msg("received shutdown signal")
		cancel()
	}()

	if noMenubar {
		return d.Run(ctx)
	}

	go func() {
		if err := d.Run(ctx); err != nil {
			logger.Error().Err(err).Msg("daemon error")
		}
		os.Exit(0)
	}()

	menubar.Run(d)
	return nil
}
