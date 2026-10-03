// Package resolvers maps the services onto the GraphQL schema.
package resolvers

import (
	"context"
	"strings"
	"time"

	"github.com/weeb-vip/notifications-service/graph/model"
	"github.com/weeb-vip/notifications-service/http/handlers/requestinfo"
	activitiesrepo "github.com/weeb-vip/notifications-service/internal/db/repositories/activities"
	notificationsrepo "github.com/weeb-vip/notifications-service/internal/db/repositories/notifications"
	"github.com/weeb-vip/notifications-service/internal/events"
	"github.com/weeb-vip/notifications-service/internal/services/feed"
	"github.com/weeb-vip/notifications-service/internal/services/notifications"
	"github.com/weeb-vip/notifications-service/metrics"
	"github.com/weeb-vip/notifications-service/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// ErrUnauthenticated is returned when a viewer-scoped resolver has no viewer.
// The @Authenticated directive rejects these first; this is the second line.
type ErrUnauthenticated struct{}

func (ErrUnauthenticated) Error() string { return "You must be signed in to do that" }

func timed[T any](ctx context.Context, name string, fn func(ctx context.Context) (T, error)) (T, error) {
	ctx, span := tracing.GetTracer(ctx).Start(ctx, name)
	span.SetAttributes(attribute.String("resolver.name", name))
	defer span.End()
	start := time.Now()

	out, err := fn(ctx)
	result := metrics.Success
	if err != nil {
		result = metrics.Error
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	metrics.GetAppMetrics().ResolverMetric(float64(time.Since(start).Microseconds())/1000, name, result)

	return out, err
}

func viewer(ctx context.Context) (string, error) {
	req := requestinfo.FromContext(ctx)
	if req.UserID == nil {
		return "", ErrUnauthenticated{}
	}

	return *req.UserID, nil
}

var activityTypes = map[string]model.ActivityType{
	events.AnimeAdded:         model.ActivityTypeAnimeAdded,
	events.AnimeStatusChanged: model.ActivityTypeAnimeStatusChanged,
	events.AnimeScored:        model.ActivityTypeAnimeScored,
	events.WorkAdded:          model.ActivityTypeWorkAdded,
	events.WorkStatusChanged:  model.ActivityTypeWorkStatusChanged,
	events.WorkScored:         model.ActivityTypeWorkScored,
}

// ToActivity maps a stored activity to the API type. Removals and unknown
// types, which a feed never shows, come back nil.
func ToActivity(a *activitiesrepo.Activity) *model.Activity {
	typ, ok := activityTypes[a.Type]
	if !ok {
		return nil
	}
	out := &model.Activity{
		ID:             a.ID,
		Type:           typ,
		Actor:          &model.PublicUser{ID: a.ActorID},
		Status:         a.Status,
		PreviousStatus: a.PreviousStatus,
		Score:          a.Score,
		OccurredAt:     a.OccurredAt.UTC().Format(time.RFC3339),
	}
	if a.AnimeID != nil {
		out.Anime = &model.Anime{ID: *a.AnimeID}
	}
	if a.WorkID != nil {
		out.Work = &model.Work{ID: *a.WorkID}
	}

	return out
}

func toActivityPage(items []*activitiesrepo.Activity, total int64, page, limit int) *model.ActivityPaginated {
	out := &model.ActivityPaginated{Page: page, Limit: limit, Total: int(total), Activities: make([]*model.Activity, 0, len(items))}
	for _, item := range items {
		if a := ToActivity(item); a != nil {
			out.Activities = append(out.Activities, a)
		}
	}

	return out
}

// Notification type names: the stored lowercase form and the API enum.
func toNotificationType(stored string) model.NotificationType {
	return model.NotificationType(strings.ToUpper(stored))
}

func fromNotificationType(t model.NotificationType) string { return strings.ToLower(string(t)) }

func toChannel(stored string) model.NotificationChannel {
	return model.NotificationChannel(strings.ToUpper(stored))
}

func fromChannel(c model.NotificationChannel) string { return strings.ToLower(string(c)) }

func toPlatform(stored string) model.PushPlatform { return model.PushPlatform(strings.ToUpper(stored)) }

func fromPlatform(p model.PushPlatform) string { return strings.ToLower(string(p)) }

// ToNotification maps an inbox row to the API type.
func ToNotification(n *notificationsrepo.Notification) *model.Notification {
	out := &model.Notification{
		ID:        n.ID,
		Type:      toNotificationType(n.Type),
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
	}
	if n.ActorID != nil {
		out.Actor = &model.PublicUser{ID: *n.ActorID}
	}
	if len(n.Payload) > 0 {
		payload := string(n.Payload)
		out.Payload = &payload
	}
	if n.ReadAt != nil {
		readAt := n.ReadAt.UTC().Format(time.RFC3339)
		out.ReadAt = &readAt
	}

	return out
}

func toPreference(p *notificationsrepo.Preference) *model.NotificationPreference {
	return &model.NotificationPreference{Type: toNotificationType(p.Type), Channel: toChannel(p.Channel), Enabled: p.Enabled}
}

func toPushSubscription(s *notificationsrepo.PushSubscription) *model.PushSubscription {
	return &model.PushSubscription{ID: s.ID, Platform: toPlatform(s.Platform), Endpoint: s.Endpoint, CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339)}
}

func Feed(ctx context.Context, svc feed.Service, page, limit int) (*model.ActivityPaginated, error) {
	return timed(ctx, "Feed", func(ctx context.Context) (*model.ActivityPaginated, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return nil, err
		}
		items, total, err := svc.Feed(ctx, userID, page, limit)
		if err != nil {
			return nil, err
		}

		return toActivityPage(items, total, page, limit), nil
	})
}

func UserActivity(ctx context.Context, svc feed.Service, userID string, page, limit int) (*model.ActivityPaginated, error) {
	return timed(ctx, "UserActivity", func(ctx context.Context) (*model.ActivityPaginated, error) {
		items, total, err := svc.UserActivity(ctx, userID, page, limit)
		if err != nil {
			return nil, err
		}

		return toActivityPage(items, total, page, limit), nil
	})
}

func ActivityByID(ctx context.Context, svc feed.Service, id string) (*model.Activity, error) {
	return timed(ctx, "ActivityByID", func(ctx context.Context) (*model.Activity, error) {
		activity, err := svc.Activity(ctx, id)
		if err != nil || activity == nil {
			return nil, err
		}

		return ToActivity(activity), nil
	})
}

func Notifications(ctx context.Context, svc notifications.Service, page, limit int, unreadOnly *bool) (*model.NotificationPaginated, error) {
	return timed(ctx, "Notifications", func(ctx context.Context) (*model.NotificationPaginated, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return nil, err
		}
		items, total, err := svc.List(ctx, userID, unreadOnly != nil && *unreadOnly, page, limit)
		if err != nil {
			return nil, err
		}
		out := &model.NotificationPaginated{Page: page, Limit: limit, Total: int(total), Notifications: make([]*model.Notification, 0, len(items))}
		for _, item := range items {
			out.Notifications = append(out.Notifications, ToNotification(item))
		}

		return out, nil
	})
}

func UnreadNotificationCount(ctx context.Context, svc notifications.Service) (int, error) {
	return timed(ctx, "UnreadNotificationCount", func(ctx context.Context) (int, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return 0, err
		}
		n, err := svc.UnreadCount(ctx, userID)

		return int(n), err
	})
}

func MarkNotificationsRead(ctx context.Context, svc notifications.Service, ids []string) (bool, error) {
	return timed(ctx, "MarkNotificationsRead", func(ctx context.Context) (bool, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return false, err
		}
		_, err = svc.MarkRead(ctx, userID, ids)

		return err == nil, err
	})
}

func MarkAllNotificationsRead(ctx context.Context, svc notifications.Service) (bool, error) {
	return timed(ctx, "MarkAllNotificationsRead", func(ctx context.Context) (bool, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return false, err
		}
		_, err = svc.MarkAllRead(ctx, userID)

		return err == nil, err
	})
}

func NotificationPreferences(ctx context.Context, svc notifications.Service) ([]*model.NotificationPreference, error) {
	return timed(ctx, "NotificationPreferences", func(ctx context.Context) ([]*model.NotificationPreference, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return nil, err
		}
		prefs, err := svc.Preferences(ctx, userID)
		if err != nil {
			return nil, err
		}
		out := make([]*model.NotificationPreference, 0, len(prefs))
		for _, p := range prefs {
			out = append(out, toPreference(p))
		}

		return out, nil
	})
}

func UpdateNotificationPreference(ctx context.Context, svc notifications.Service, typ model.NotificationType, channel model.NotificationChannel, enabled bool) (*model.NotificationPreference, error) {
	return timed(ctx, "UpdateNotificationPreference", func(ctx context.Context) (*model.NotificationPreference, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return nil, err
		}
		p, err := svc.UpdatePreference(ctx, userID, fromNotificationType(typ), fromChannel(channel), enabled)
		if err != nil {
			return nil, err
		}

		return toPreference(p), nil
	})
}

func PushSubscriptions(ctx context.Context, svc notifications.Service) ([]*model.PushSubscription, error) {
	return timed(ctx, "PushSubscriptions", func(ctx context.Context) ([]*model.PushSubscription, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return nil, err
		}
		subs, err := svc.PushSubscriptions(ctx, userID)
		if err != nil {
			return nil, err
		}
		out := make([]*model.PushSubscription, 0, len(subs))
		for _, s := range subs {
			out = append(out, toPushSubscription(s))
		}

		return out, nil
	})
}

func RegisterPushSubscription(ctx context.Context, svc notifications.Service, input model.PushSubscriptionInput) (*model.PushSubscription, error) {
	return timed(ctx, "RegisterPushSubscription", func(ctx context.Context) (*model.PushSubscription, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return nil, err
		}
		sub, err := svc.RegisterPush(ctx, &notificationsrepo.PushSubscription{
			UserID:    userID,
			Platform:  fromPlatform(input.Platform),
			Endpoint:  input.Endpoint,
			P256dh:    input.P256dh,
			Auth:      input.Auth,
			UserAgent: input.UserAgent,
		})
		if err != nil {
			return nil, err
		}

		return toPushSubscription(sub), nil
	})
}

func UnregisterPushSubscription(ctx context.Context, svc notifications.Service, endpoint string) (bool, error) {
	return timed(ctx, "UnregisterPushSubscription", func(ctx context.Context) (bool, error) {
		userID, err := viewer(ctx)
		if err != nil {
			return false, err
		}

		return svc.UnregisterPush(ctx, userID, endpoint)
	})
}
