package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/weeb-vip/notifications-service/config"
	"github.com/weeb-vip/notifications-service/internal/consumers"
	"github.com/weeb-vip/notifications-service/internal/logger"
	"github.com/weeb-vip/notifications-service/internal/wiring"
	"github.com/weeb-vip/notifications-service/tracing"
)

var consumeCmd = &cobra.Command{
	Use:   "consume",
	Short: "run a NATS consumer until stopped",
}

// One subcommand per subject, each deployed as its own pod, so a backlog on
// one never delays the other and each scales on its own.
var consumeActivityCmd = &cobra.Command{
	Use:   "activity",
	Short: "consume list activity (user-activity) into feeds",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConsumer(consumers.RunActivity)
	},
}

var consumeFollowCmd = &cobra.Command{
	Use:   "follow",
	Short: "consume follow events (user-follow) into inboxes",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConsumer(consumers.RunFollow)
	},
}

func runConsumer(run func(ctx context.Context, cfg config.Config, h consumers.Handlers) error) error {
	cfg := config.LoadConfigOrPanic()

	logger.Logger(
		logger.WithServerName(cfg.AppConfig.APPName),
		logger.WithVersion(cfg.AppConfig.Version),
		logger.WithEnvironment(cfg.AppConfig.Env),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tracedCtx, err := tracing.InitTracing(ctx)
	if err != nil {
		log := logger.FromCtx(ctx)
		log.Error().Err(err).Msg("Failed to initialize tracing")
		tracedCtx = ctx
	} else {
		defer func() {
			if err := tracing.Shutdown(context.Background()); err != nil {
				log := logger.FromCtx(tracedCtx)
				log.Error().Err(err).Msg("Error shutting down tracing")
			}
		}()
	}

	services := wiring.Build(tracedCtx, cfg, true)
	defer services.Close()

	return run(tracedCtx, cfg, consumers.Handlers{Feed: services.Feed, Notifications: services.Notifications})
}

func init() {
	consumeCmd.AddCommand(consumeActivityCmd)
	consumeCmd.AddCommand(consumeFollowCmd)
	rootCmd.AddCommand(consumeCmd)
}
