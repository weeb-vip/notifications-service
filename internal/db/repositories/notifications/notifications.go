// Package notifications stores the inbox, delivery preferences and push
// devices.
package notifications

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/weeb-vip/notifications-service/internal/db"
)

// Notification is one inbox row.
type Notification struct {
	ID         string     `gorm:"column:id;type:uuid;primaryKey"`
	EventID    string     `gorm:"column:event_id;type:uuid;not null"`
	UserID     string     `gorm:"column:user_id;not null"`
	Type       string     `gorm:"column:type;not null"`
	ActorID    *string    `gorm:"column:actor_id"`
	ActivityID *string    `gorm:"column:activity_id;type:uuid"`
	Payload    JSON       `gorm:"column:payload;type:jsonb;not null"`
	ReadAt     *time.Time `gorm:"column:read_at"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
}

// TableName implements gorm's Tabler.
func (Notification) TableName() string { return "notifications" }

// Preference is one stored override of the default for a (type, channel).
type Preference struct {
	UserID    string    `gorm:"column:user_id;primaryKey"`
	Type      string    `gorm:"column:type;primaryKey"`
	Channel   string    `gorm:"column:channel;primaryKey"`
	Enabled   bool      `gorm:"column:enabled;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;autoUpdateTime"`
}

// TableName implements gorm's Tabler.
func (Preference) TableName() string { return "notification_preferences" }

// PushSubscription is one device or browser registered for push.
type PushSubscription struct {
	ID         string    `gorm:"column:id;type:uuid;primaryKey"`
	UserID     string    `gorm:"column:user_id;not null"`
	Platform   string    `gorm:"column:platform;not null"`
	Endpoint   string    `gorm:"column:endpoint;not null"`
	P256dh     *string   `gorm:"column:p256dh"`
	Auth       *string   `gorm:"column:auth"`
	UserAgent  *string   `gorm:"column:user_agent"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	LastSeenAt time.Time `gorm:"column:last_seen_at;not null"`
}

// TableName implements gorm's Tabler.
func (PushSubscription) TableName() string { return "push_subscriptions" }

// Repository is the storage for the inbox, preferences and devices.
type Repository interface {
	// Insert stores a notification and reports whether it was new; a second
	// delivery of the same event to the same user is a no-op.
	Insert(ctx context.Context, notification *Notification) (bool, error)
	List(ctx context.Context, userID string, unreadOnly bool, page, limit int) ([]*Notification, int64, error)
	UnreadCount(ctx context.Context, userID string) (int64, error)
	// MarkRead marks the given ids read where they belong to userID.
	MarkRead(ctx context.Context, userID string, ids []string, at time.Time) (int64, error)
	MarkAllRead(ctx context.Context, userID string, at time.Time) (int64, error)
	// DeleteByActor removes userID's notifications caused by actorID of the
	// given types, e.g. a withdrawn follow request.
	DeleteByActor(ctx context.Context, userID, actorID string, types []string) (int64, error)

	Preferences(ctx context.Context, userID string) ([]*Preference, error)
	UpsertPreference(ctx context.Context, preference *Preference) error

	PushSubscriptions(ctx context.Context, userID string) ([]*PushSubscription, error)
	UpsertPushSubscription(ctx context.Context, subscription *PushSubscription) error
	DeletePushSubscription(ctx context.Context, userID, endpoint string) (int64, error)
}

type repository struct {
	db *db.DB
}

// New returns the Postgres implementation.
func New(database *db.DB) Repository {
	return &repository{db: database}
}

func (r *repository) Insert(ctx context.Context, notification *Notification) (bool, error) {
	if len(notification.Payload) == 0 {
		notification.Payload = JSON("{}")
	}
	result := r.db.DB.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "event_id"}}, DoNothing: true}).
		Create(notification)
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected == 1, nil
}

func (r *repository) List(ctx context.Context, userID string, unreadOnly bool, page, limit int) ([]*Notification, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	query := r.db.DB.WithContext(ctx).Model(&Notification{}).Where("user_id = ?", userID)
	if unreadOnly {
		query = query.Where("read_at IS NULL")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var notifications []*Notification
	err := query.Order("created_at DESC, id DESC").Offset((page - 1) * limit).Limit(limit).Find(&notifications).Error
	if err != nil {
		return nil, 0, err
	}

	return notifications, total, nil
}

func (r *repository) UnreadCount(ctx context.Context, userID string) (int64, error) {
	var n int64
	err := r.db.DB.WithContext(ctx).Model(&Notification{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&n).Error

	return n, err
}

func (r *repository) MarkRead(ctx context.Context, userID string, ids []string, at time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.DB.WithContext(ctx).Model(&Notification{}).
		Where("user_id = ? AND id IN ? AND read_at IS NULL", userID, ids).
		Update("read_at", at)

	return result.RowsAffected, result.Error
}

func (r *repository) MarkAllRead(ctx context.Context, userID string, at time.Time) (int64, error) {
	result := r.db.DB.WithContext(ctx).Model(&Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Update("read_at", at)

	return result.RowsAffected, result.Error
}

func (r *repository) DeleteByActor(ctx context.Context, userID, actorID string, types []string) (int64, error) {
	if len(types) == 0 {
		return 0, nil
	}
	result := r.db.DB.WithContext(ctx).
		Where("user_id = ? AND actor_id = ? AND type IN ?", userID, actorID, types).
		Delete(&Notification{})

	return result.RowsAffected, result.Error
}

func (r *repository) Preferences(ctx context.Context, userID string) ([]*Preference, error) {
	var preferences []*Preference
	err := r.db.DB.WithContext(ctx).Where("user_id = ?", userID).Find(&preferences).Error

	return preferences, err
}

func (r *repository) UpsertPreference(ctx context.Context, preference *Preference) error {
	preference.UpdatedAt = time.Now().UTC()

	return r.db.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "type"}, {Name: "channel"}},
			DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
		}).
		Create(preference).Error
}

func (r *repository) PushSubscriptions(ctx context.Context, userID string) ([]*PushSubscription, error) {
	var subscriptions []*PushSubscription
	err := r.db.DB.WithContext(ctx).Where("user_id = ?", userID).Order("created_at ASC").Find(&subscriptions).Error

	return subscriptions, err
}

func (r *repository) UpsertPushSubscription(ctx context.Context, subscription *PushSubscription) error {
	subscription.LastSeenAt = time.Now().UTC()

	return r.db.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "endpoint"}},
			DoUpdates: clause.AssignmentColumns([]string{"user_id", "platform", "p256dh", "auth", "user_agent", "last_seen_at"}),
		}).
		Create(subscription).Error
}

func (r *repository) DeletePushSubscription(ctx context.Context, userID, endpoint string) (int64, error) {
	result := r.db.DB.WithContext(ctx).Where("user_id = ? AND endpoint = ?", userID, endpoint).Delete(&PushSubscription{})

	return result.RowsAffected, result.Error
}

// notFound reports gorm's not-found as a nil row.
func notFound(err error) bool { return err == gorm.ErrRecordNotFound }
