// Package notifications owns the inbox: what each follow event means for
// whom, what the viewer may read and change, and which deliveries to request.
package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/weeb-vip/notifications-service/internal/db/repositories/notifications"
	"github.com/weeb-vip/notifications-service/internal/deliver"
	"github.com/weeb-vip/notifications-service/internal/events"
)

// Notification types this service creates.
const (
	TypeFollowRequested = "follow_requested"
	TypeFollowAccepted  = "follow_accepted"
	TypeNewFollower     = "new_follower"
)

// Types lists every notification type, in the order the preferences matrix
// shows them.
var Types = []string{TypeFollowRequested, TypeFollowAccepted, TypeNewFollower}

// Channels a notification can go out on.
const (
	ChannelInApp = "in_app"
	ChannelPush  = "push"
	ChannelEmail = "email"
)

// ChannelList lists every channel, in matrix order.
var ChannelList = []string{ChannelInApp, ChannelPush, ChannelEmail}

// DefaultEnabled is the preference for a (type, channel) with no stored row:
// in-app on, everything that leaves the site off until the user opts in.
func DefaultEnabled(channel string) bool { return channel == ChannelInApp }

// Service is the inbox's behaviour.
type Service interface {
	// HandleFollow turns a follow event into zero, one or two notifications
	// and the deliveries they call for. Redeliveries are no-ops.
	HandleFollow(ctx context.Context, event events.Follow) error

	List(ctx context.Context, userID string, unreadOnly bool, page, limit int) ([]*notifications.Notification, int64, error)
	UnreadCount(ctx context.Context, userID string) (int64, error)
	MarkRead(ctx context.Context, userID string, ids []string) (int64, error)
	MarkAllRead(ctx context.Context, userID string) (int64, error)

	// Preferences returns the full type x channel matrix with defaults filled in.
	Preferences(ctx context.Context, userID string) ([]*notifications.Preference, error)
	UpdatePreference(ctx context.Context, userID, notificationType, channel string, enabled bool) (*notifications.Preference, error)

	PushSubscriptions(ctx context.Context, userID string) ([]*notifications.PushSubscription, error)
	RegisterPush(ctx context.Context, subscription *notifications.PushSubscription) (*notifications.PushSubscription, error)
	UnregisterPush(ctx context.Context, userID, endpoint string) (bool, error)
}

// FeedHooks are the feed-side effects of follow events, kept behind an
// interface so this service does not depend on the feed package.
type FeedHooks interface {
	Seed(ctx context.Context, followerID, actorID string) error
	Unfollow(ctx context.Context, followerID, actorID string) error
}

type service struct {
	repo    notifications.Repository
	feed    FeedHooks
	deliver deliver.Publisher
	log     zerolog.Logger
	now     func() time.Time
}

// New wires the service.
func New(repo notifications.Repository, feed FeedHooks, publisher deliver.Publisher, log zerolog.Logger) Service {
	return &service{repo: repo, feed: feed, deliver: publisher, log: log, now: func() time.Time { return time.Now().UTC() }}
}

// recipient is one notification a follow event produces.
type recipient struct {
	userID string
	typ    string
}

// plan says who a follow event notifies. The event's auto_accepted flag is
// what separates "your request was approved" from "someone followed you".
func plan(event events.Follow) []recipient {
	switch event.Type {
	case events.FollowRequested:
		return []recipient{{event.FolloweeID, TypeFollowRequested}}
	case events.FollowAccepted:
		if event.AutoAccepted {
			return []recipient{{event.FolloweeID, TypeNewFollower}}
		}

		return []recipient{{event.FollowerID, TypeFollowAccepted}, {event.FolloweeID, TypeNewFollower}}
	default:
		return nil
	}
}

func (s *service) HandleFollow(ctx context.Context, event events.Follow) error {
	if _, err := uuid.Parse(event.ID); err != nil {
		return fmt.Errorf("notifications: event id %q is not a uuid: %w", event.ID, err)
	}
	if event.FollowerID == "" || event.FolloweeID == "" {
		return fmt.Errorf("notifications: event %s missing follower_id or followee_id", event.ID)
	}

	switch event.Type {
	case events.FollowRequested, events.FollowAccepted:
		for _, r := range plan(event) {
			if err := s.notify(ctx, event, r); err != nil {
				return err
			}
		}
		if event.Type == events.FollowAccepted {
			if err := s.feed.Seed(ctx, event.FollowerID, event.FolloweeID); err != nil {
				return fmt.Errorf("notifications: seed feed: %w", err)
			}
		}

		return nil
	case events.FollowDeclined, events.Unfollowed, events.FollowerRemoved:
		if err := s.feed.Unfollow(ctx, event.FollowerID, event.FolloweeID); err != nil {
			return fmt.Errorf("notifications: clear feed: %w", err)
		}
		// A withdrawn or declined request no longer needs answering.
		if _, err := s.repo.DeleteByActor(ctx, event.FolloweeID, event.FollowerID, []string{TypeFollowRequested}); err != nil {
			return fmt.Errorf("notifications: clear request: %w", err)
		}

		return nil
	default:
		s.log.Warn().Str("type", event.Type).Str("event_id", event.ID).Msg("ignoring unknown follow event type")

		return nil
	}
}

func (s *service) notify(ctx context.Context, event events.Follow, r recipient) error {
	payload, _ := json.Marshal(map[string]any{
		"follower_id": event.FollowerID,
		"followee_id": event.FolloweeID,
	})
	actor := event.FollowerID
	if r.typ == TypeFollowAccepted {
		actor = event.FolloweeID
	}
	notification := &notifications.Notification{
		ID:        uuid.New().String(),
		EventID:   event.ID,
		UserID:    r.userID,
		Type:      r.typ,
		ActorID:   &actor,
		Payload:   notifications.JSON(payload),
		CreatedAt: s.now(),
	}

	created, err := s.repo.Insert(ctx, notification)
	if err != nil {
		return fmt.Errorf("notifications: insert: %w", err)
	}
	if !created {
		return nil
	}

	return s.requestDelivery(ctx, notification)
}

// requestDelivery publishes to the delivery workers for every channel the
// recipient enabled beyond in-app. A publish failure is logged, not returned:
// the notification is in the inbox, and failing the consumer would create it
// again on retry and still not deliver.
func (s *service) requestDelivery(ctx context.Context, notification *notifications.Notification) error {
	prefs, err := s.repo.Preferences(ctx, notification.UserID)
	if err != nil {
		return fmt.Errorf("notifications: preferences: %w", err)
	}
	channels := enabledChannels(prefs, notification.Type)
	if len(channels) == 0 {
		return nil
	}

	err = s.deliver.Publish(ctx, events.Delivery{
		NotificationID: notification.ID,
		UserID:         notification.UserID,
		Type:           notification.Type,
		Channels:       channels,
	})
	if err != nil {
		s.log.Error().Err(err).Str("notification_id", notification.ID).Msg("failed to request delivery")
	}

	return nil
}

// enabledChannels returns the non-in-app channels enabled for a type.
func enabledChannels(prefs []*notifications.Preference, notificationType string) []string {
	enabled := map[string]bool{}
	for _, channel := range ChannelList {
		enabled[channel] = DefaultEnabled(channel)
	}
	for _, p := range prefs {
		if p.Type == notificationType {
			enabled[p.Channel] = p.Enabled
		}
	}
	var out []string
	for _, channel := range []string{ChannelPush, ChannelEmail} {
		if enabled[channel] {
			out = append(out, channel)
		}
	}

	return out
}

func (s *service) List(ctx context.Context, userID string, unreadOnly bool, page, limit int) ([]*notifications.Notification, int64, error) {
	items, total, err := s.repo.List(ctx, userID, unreadOnly, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []*notifications.Notification{}
	}

	return items, total, nil
}

func (s *service) UnreadCount(ctx context.Context, userID string) (int64, error) {
	return s.repo.UnreadCount(ctx, userID)
}

func (s *service) MarkRead(ctx context.Context, userID string, ids []string) (int64, error) {
	return s.repo.MarkRead(ctx, userID, ids, s.now())
}

func (s *service) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	return s.repo.MarkAllRead(ctx, userID, s.now())
}

func (s *service) Preferences(ctx context.Context, userID string) ([]*notifications.Preference, error) {
	stored, err := s.repo.Preferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	overrides := map[string]bool{}
	for _, p := range stored {
		overrides[p.Type+"/"+p.Channel] = p.Enabled
	}

	matrix := make([]*notifications.Preference, 0, len(Types)*len(ChannelList))
	for _, typ := range Types {
		for _, channel := range ChannelList {
			enabled, ok := overrides[typ+"/"+channel]
			if !ok {
				enabled = DefaultEnabled(channel)
			}
			matrix = append(matrix, &notifications.Preference{UserID: userID, Type: typ, Channel: channel, Enabled: enabled})
		}
	}

	return matrix, nil
}

func (s *service) UpdatePreference(ctx context.Context, userID, notificationType, channel string, enabled bool) (*notifications.Preference, error) {
	if !contains(Types, notificationType) {
		return nil, fmt.Errorf("notifications: unknown type %q", notificationType)
	}
	if !contains(ChannelList, channel) {
		return nil, fmt.Errorf("notifications: unknown channel %q", channel)
	}
	preference := &notifications.Preference{UserID: userID, Type: notificationType, Channel: channel, Enabled: enabled}
	if err := s.repo.UpsertPreference(ctx, preference); err != nil {
		return nil, err
	}

	return preference, nil
}

func (s *service) PushSubscriptions(ctx context.Context, userID string) ([]*notifications.PushSubscription, error) {
	subs, err := s.repo.PushSubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if subs == nil {
		subs = []*notifications.PushSubscription{}
	}

	return subs, nil
}

func (s *service) RegisterPush(ctx context.Context, subscription *notifications.PushSubscription) (*notifications.PushSubscription, error) {
	if subscription.Endpoint == "" {
		return nil, fmt.Errorf("notifications: endpoint is required")
	}
	if subscription.ID == "" {
		subscription.ID = uuid.New().String()
	}
	if err := s.repo.UpsertPushSubscription(ctx, subscription); err != nil {
		return nil, err
	}
	// The upsert may have kept an existing row's id; read back by endpoint.
	subs, err := s.repo.PushSubscriptions(ctx, subscription.UserID)
	if err != nil {
		return nil, err
	}
	for _, sub := range subs {
		if sub.Endpoint == subscription.Endpoint {
			return sub, nil
		}
	}

	return subscription, nil
}

func (s *service) UnregisterPush(ctx context.Context, userID, endpoint string) (bool, error) {
	n, err := s.repo.DeletePushSubscription(ctx, userID, endpoint)

	return n > 0, err
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}

	return false
}
