// Package wiring builds the service graph once, for the HTTP server and the
// consumers alike.
package wiring

import (
	"context"

	epnats "github.com/ThatCatDev/ep/v2/drivers/nats"

	"github.com/weeb-vip/notifications-service/config"
	"github.com/weeb-vip/notifications-service/internal/clients/userservice"
	"github.com/weeb-vip/notifications-service/internal/db"
	activitiesrepo "github.com/weeb-vip/notifications-service/internal/db/repositories/activities"
	notificationsrepo "github.com/weeb-vip/notifications-service/internal/db/repositories/notifications"
	"github.com/weeb-vip/notifications-service/internal/deliver"
	"github.com/weeb-vip/notifications-service/internal/logger"
	"github.com/weeb-vip/notifications-service/internal/services/feed"
	"github.com/weeb-vip/notifications-service/internal/services/notifications"
)

// Services is everything the API and the consumers need.
type Services struct {
	Feed          feed.Service
	Notifications notifications.Service
	Close         func()
}

// Build connects the database, the user-service client and, when withNats is
// set, the delivery publisher. The HTTP server passes false: it never
// publishes, so it should not hold a NATS connection it does not use.
func Build(ctx context.Context, cfg config.Config, withNats bool) *Services {
	return BuildWith(ctx, cfg, db.NewDatabase(cfg.DBConfig), userservice.New(cfg.UserService.URL), withNats)
}

// BuildWith is Build over a given database and user-service client, which is
// how the end-to-end tests share a connection and substitute user-service.
func BuildWith(ctx context.Context, cfg config.Config, database *db.DB, users userservice.Client, withNats bool) *Services {
	log := logger.FromCtx(ctx)

	var publisher deliver.Publisher = deliver.Noop{}
	closeFn := func() {}
	if withNats {
		driver := epnats.NewNatsDriver(&epnats.Config{URL: cfg.NatsConfig.URL})
		publisher = deliver.NewNats(driver)
		closeFn = func() { _ = driver.Close() }
	}

	feedService := feed.New(activitiesrepo.New(database), users, feed.Config{}, log)
	notificationsService := notifications.New(notificationsrepo.New(database), feedService, publisher, log)

	return &Services{Feed: feedService, Notifications: notificationsService, Close: closeFn}
}
