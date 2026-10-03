// Package deliver hands notifications to the push and email workers over NATS.
package deliver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ThatCatDev/ep/v2/drivers"
	epnats "github.com/ThatCatDev/ep/v2/drivers/nats"

	"github.com/weeb-vip/notifications-service/internal/events"
)

// Publisher enqueues one delivery. Errors are logged by the caller and never
// fail the inbox write: a push that is late beats a notification that is lost.
type Publisher interface {
	Publish(ctx context.Context, delivery events.Delivery) error
}

// Noop drops deliveries; for tests and for deployments with no workers yet.
type Noop struct{}

// Publish implements Publisher.
func (Noop) Publish(context.Context, events.Delivery) error { return nil }

type natsPublisher struct {
	driver drivers.Driver[*epnats.Message]
}

// NewNats publishes on events.DeliverSubject through an ep NATS driver.
func NewNats(driver drivers.Driver[*epnats.Message]) Publisher {
	return &natsPublisher{driver: driver}
}

func (p *natsPublisher) Publish(ctx context.Context, delivery events.Delivery) error {
	body, err := json.Marshal(delivery)
	if err != nil {
		return fmt.Errorf("deliver: marshal: %w", err)
	}

	return p.driver.Produce(ctx, events.DeliverSubject, &epnats.Message{
		Subject: events.DeliverSubject,
		Data:    body,
		Headers: map[string]string{"Nats-Msg-Id": delivery.NotificationID},
	})
}
