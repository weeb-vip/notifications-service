// Package activities stores activity events and the per-user feed built from
// them.
package activities

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/weeb-vip/notifications-service/internal/db"
)

// Activity is one row of activities: a thing someone did to a list.
type Activity struct {
	ID             string    `gorm:"column:id;type:uuid;primaryKey"`
	EventID        string    `gorm:"column:event_id;type:uuid;not null"`
	ActorID        string    `gorm:"column:actor_id;not null"`
	Type           string    `gorm:"column:type;not null"`
	AnimeID        *string   `gorm:"column:anime_id"`
	WorkID         *string   `gorm:"column:work_id"`
	Status         *string   `gorm:"column:status"`
	PreviousStatus *string   `gorm:"column:previous_status"`
	Score          *float64  `gorm:"column:score"`
	OccurredAt     time.Time `gorm:"column:occurred_at;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null;autoCreateTime"`
}

// TableName implements gorm's Tabler.
func (Activity) TableName() string { return "activities" }

// FeedItem is one row of feed_items: an activity in one recipient's feed.
type FeedItem struct {
	UserID     string    `gorm:"column:user_id;primaryKey"`
	ActivityID string    `gorm:"column:activity_id;type:uuid;primaryKey"`
	ActorID    string    `gorm:"column:actor_id;not null"`
	OccurredAt time.Time `gorm:"column:occurred_at;not null"`
}

// TableName implements gorm's Tabler.
func (FeedItem) TableName() string { return "feed_items" }

// Repository is the storage for activities and feeds.
type Repository interface {
	// Insert stores an activity and reports whether it was new. An activity
	// whose event_id is already stored is left alone and reported as not new,
	// which is how a redelivered message becomes a no-op.
	Insert(ctx context.Context, activity *Activity) (bool, error)
	FindByID(ctx context.Context, id string) (*Activity, error)
	// FanOut adds the activity to each recipient's feed, skipping recipients
	// that already have it.
	FanOut(ctx context.Context, activity *Activity, recipientIDs []string) error
	// Feed pages a recipient's feed newest first.
	Feed(ctx context.Context, userID string, page, limit int) ([]*Activity, int64, error)
	// ByActor pages one actor's own activity newest first, removals excluded.
	ByActor(ctx context.Context, actorID string, page, limit int) ([]*Activity, int64, error)
	// RecentByActor returns the actor's newest fan-out-worthy activities, for
	// seeding a new follower's feed.
	RecentByActor(ctx context.Context, actorID string, limit int) ([]*Activity, error)
	// RemoveActorFromFeed drops every item by actorID from userID's feed.
	RemoveActorFromFeed(ctx context.Context, userID, actorID string) (int64, error)
	// Prune deletes activities older than before, and their feed items with them.
	Prune(ctx context.Context, before time.Time) (int64, error)
}

type repository struct {
	db *db.DB
}

// New returns the Postgres implementation.
func New(database *db.DB) Repository {
	return &repository{db: database}
}

// FanOutBatch is how many feed rows one INSERT carries. Postgres takes 65535
// parameters per statement; four columns leave plenty of room at 500.
const FanOutBatch = 500

func (r *repository) Insert(ctx context.Context, activity *Activity) (bool, error) {
	result := r.db.DB.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).
		Create(activity)
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected == 1, nil
}

func (r *repository) FindByID(ctx context.Context, id string) (*Activity, error) {
	var activity Activity
	err := r.db.DB.WithContext(ctx).First(&activity, "id = ?", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &activity, nil
}

func (r *repository) FanOut(ctx context.Context, activity *Activity, recipientIDs []string) error {
	if len(recipientIDs) == 0 {
		return nil
	}
	items := make([]FeedItem, 0, len(recipientIDs))
	for _, userID := range recipientIDs {
		items = append(items, FeedItem{
			UserID:     userID,
			ActivityID: activity.ID,
			ActorID:    activity.ActorID,
			OccurredAt: activity.OccurredAt,
		})
	}

	return r.db.DB.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(items, FanOutBatch).Error
}

func page(p, limit int) (int, int) {
	if p < 1 {
		p = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	return p, limit
}

func (r *repository) Feed(ctx context.Context, userID string, p, limit int) ([]*Activity, int64, error) {
	p, limit = page(p, limit)
	database := r.db.DB.WithContext(ctx)

	var total int64
	if err := database.Model(&FeedItem{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// One range scan on idx_feed_items_user_time, then the activities by
	// primary key. The join keeps it one round trip.
	var activities []*Activity
	err := database.Table("activities").
		Select("activities.*").
		Joins("JOIN feed_items f ON f.activity_id = activities.id").
		Where("f.user_id = ?", userID).
		Order("f.occurred_at DESC, f.activity_id DESC").
		Offset((p - 1) * limit).
		Limit(limit).
		Find(&activities).Error
	if err != nil {
		return nil, 0, err
	}

	return activities, total, nil
}

// removals are kept for history but are not something to show.
var shown = []string{"anime.added", "anime.status_changed", "anime.scored", "work.added", "work.status_changed", "work.scored"}

func (r *repository) ByActor(ctx context.Context, actorID string, p, limit int) ([]*Activity, int64, error) {
	p, limit = page(p, limit)
	database := r.db.DB.WithContext(ctx).Model(&Activity{}).Where("actor_id = ? AND type IN ?", actorID, shown)

	var total int64
	if err := database.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var activities []*Activity
	err := database.Order("occurred_at DESC, id DESC").Offset((p - 1) * limit).Limit(limit).Find(&activities).Error
	if err != nil {
		return nil, 0, err
	}

	return activities, total, nil
}

func (r *repository) RecentByActor(ctx context.Context, actorID string, limit int) ([]*Activity, error) {
	if limit < 1 {
		limit = 20
	}
	var activities []*Activity
	err := r.db.DB.WithContext(ctx).
		Where("actor_id = ? AND type IN ?", actorID, shown).
		Order("occurred_at DESC, id DESC").
		Limit(limit).
		Find(&activities).Error

	return activities, err
}

func (r *repository) RemoveActorFromFeed(ctx context.Context, userID, actorID string) (int64, error) {
	result := r.db.DB.WithContext(ctx).Where("user_id = ? AND actor_id = ?", userID, actorID).Delete(&FeedItem{})

	return result.RowsAffected, result.Error
}

func (r *repository) Prune(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.DB.WithContext(ctx).Where("occurred_at < ?", before).Delete(&Activity{})

	return result.RowsAffected, result.Error
}
