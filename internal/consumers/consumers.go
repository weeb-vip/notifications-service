// Package consumers runs the NATS JetStream consumers: one for list activity,
// one for follow events. Each is a main processor plus a retry processor in
// the same process, with exhausted retries parked on a dead-letter subject,
// following user-service's user-created consumer.
package consumers

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"

	"github.com/ThatCatDev/ep/v2/drivers"
	epnats "github.com/ThatCatDev/ep/v2/drivers/nats"
	"github.com/ThatCatDev/ep/v2/event"
	"github.com/ThatCatDev/ep/v2/middlewares/nats/backoffretry"
	"github.com/ThatCatDev/ep/v2/processor"

	"github.com/weeb-vip/notifications-service/config"
	"github.com/weeb-vip/notifications-service/internal/events"
	"github.com/weeb-vip/notifications-service/internal/logger"
	"github.com/weeb-vip/notifications-service/internal/services/feed"
	"github.com/weeb-vip/notifications-service/internal/services/notifications"
)

const (
	maxRetries     = 3
	retryHeaderKey = "retry"
)

// Handlers is what the consumers call into.
type Handlers struct {
	Feed          feed.Service
	Notifications notifications.Service
}

// ProcessActivity is the activity consumer's unit of work, exposed for tests.
// A malformed event is an error, so it goes through retry to the dead-letter
// subject where someone can look at it, rather than being acked and lost.
func ProcessActivity(h Handlers) func(ctx context.Context, data event.Event[*epnats.Message, events.Activity]) (event.Event[*epnats.Message, events.Activity], error) {
	return func(ctx context.Context, data event.Event[*epnats.Message, events.Activity]) (event.Event[*epnats.Message, events.Activity], error) {
		log := logger.FromCtx(ctx)
		if data.Payload.ID == "" {
			return data, errors.New("activity event has no id")
		}
		outcome, err := h.Feed.Ingest(ctx, data.Payload)
		if err != nil {
			log.Error().Err(err).Str("event_id", data.Payload.ID).Msg("failed to ingest activity")

			return data, err
		}
		log.Info().
			Str("event_id", data.Payload.ID).
			Str("type", data.Payload.Type).
			Bool("stored", outcome.Stored).
			Int("fanned_out", outcome.FannedOut).
			Str("skip", outcome.SkipReason).
			Msg("activity ingested")

		return data, nil
	}
}

// ProcessFollow is the follow consumer's unit of work, exposed for tests.
func ProcessFollow(h Handlers) func(ctx context.Context, data event.Event[*epnats.Message, events.Follow]) (event.Event[*epnats.Message, events.Follow], error) {
	return func(ctx context.Context, data event.Event[*epnats.Message, events.Follow]) (event.Event[*epnats.Message, events.Follow], error) {
		log := logger.FromCtx(ctx)
		if data.Payload.ID == "" {
			return data, errors.New("follow event has no id")
		}
		if err := h.Notifications.HandleFollow(ctx, data.Payload); err != nil {
			log.Error().Err(err).Str("event_id", data.Payload.ID).Msg("failed to handle follow event")

			return data, err
		}
		log.Info().Str("event_id", data.Payload.ID).Str("type", data.Payload.Type).Msg("follow event handled")

		return data, nil
	}
}

// RunActivity consumes events.ActivitySubject until ctx is cancelled.
func RunActivity(ctx context.Context, cfg config.Config, h Handlers) error {
	return run[events.Activity](ctx, cfg, events.ActivitySubject, ProcessActivity(h))
}

// RunFollow consumes events.FollowSubject until ctx is cancelled.
func RunFollow(ctx context.Context, cfg config.Config, h Handlers) error {
	return run[events.Follow](ctx, cfg, events.FollowSubject, ProcessFollow(h))
}

func run[M any](ctx context.Context, cfg config.Config, subject string, process processor.Process[*epnats.Message, M]) error {
	log := logger.FromCtx(ctx)
	retrySubject := subject + "-retry"
	dlqSubject := subject + "-dlq"
	group := cfg.NatsConfig.ConsumerGroupName + "-" + subject

	driver := epnats.NewNatsDriver(&epnats.Config{
		URL:                     cfg.NatsConfig.URL,
		ConsumerGroupName:       group,
		StreamName:              cfg.NatsConfig.StreamName,
		ConsumerAutoOffsetReset: &cfg.NatsConfig.Offset,
	})
	defer closeDriver(ctx, driver, "NATS driver")

	// Its own driver: the durable consumer name is driver-level, so one driver
	// cannot consume two subjects without reconfiguring itself.
	retryDriver := epnats.NewNatsDriver(&epnats.Config{
		URL:                     cfg.NatsConfig.URL,
		ConsumerGroupName:       group + "-retry",
		StreamName:              cfg.NatsConfig.StreamName,
		ConsumerAutoOffsetReset: &cfg.NatsConfig.Offset,
	})
	defer closeDriver(ctx, retryDriver, "NATS retry driver")

	mainProcessor := processor.NewProcessor[*epnats.Message, M](driver, subject, process).
		AddMiddleware(backoffretry.NewBackoffRetry[M](driver, backoffretry.Config{
			MaxRetries: maxRetries,
			HeaderKey:  retryHeaderKey,
			RetryQueue: retrySubject,
		}).Process)

	// Exhausted retries go to the dead-letter subject rather than being
	// dropped, so a permanently failing event leaves a record.
	retryProcessor := processor.NewProcessor[*epnats.Message, M](retryDriver, retrySubject, process).
		AddMiddleware(backoffretry.NewBackoffRetry[M](retryDriver, backoffretry.Config{
			MaxRetries: maxRetries,
			HeaderKey:  retryHeaderKey,
			RetryQueue: dlqSubject,
		}).Process)

	log.Info().Str("subject", subject).Str("retry_subject", retrySubject).Str("dlq_subject", dlqSubject).Msg("Starting NATS processors")

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return mainProcessor.Run(egCtx) })
	eg.Go(func() error { return retryProcessor.Run(egCtx) })

	if err := eg.Wait(); err != nil && ctx.Err() == nil {
		log.Error().Err(err).Str("subject", subject).Msg("Error consuming messages")

		return err
	}

	return nil
}

func closeDriver(ctx context.Context, driver drivers.Driver[*epnats.Message], name string) {
	log := logger.FromCtx(ctx)
	if err := driver.Close(); err != nil {
		log.Error().Err(err).Msgf("Error closing %s", name)

		return
	}
	log.Info().Msgf("%s closed", name)
}
