package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sockPath := config.SocketPath()
	srv := daemon.NewServer(d, sockPath, logger)
	srv.SetOnShutdown(func() {
		logger.Info().Msg("shutdown requested, exiting")
		cancel()
	})
	// Only the menu-bar instance (launchd agent / app launch) may displace a
	// live daemon. Headless ensureDaemon spawns just yield, else two
	// concurrent CLI calls duel and kill each other's fresh daemon.
	var startErr error
	if noMenubar {
		startErr = srv.Start()
	} else {
		startErr = srv.StartTakeover(5 * time.Second)
	}
	if startErr != nil {
		return startErr
	}
	defer srv.Stop()

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
		// menuet's RunApplication never returns, so exit here — but release
		// the socket first since os.Exit skips the deferred Stop.
		srv.Stop()
		os.Exit(0)
	}()

	menubar.Run(d)
	return nil
}
