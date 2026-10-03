// Package events mirrors the payloads other services publish. The structs are
// copied rather than imported so a producer's internal refactor cannot change
// what this service accepts; the JSON is the contract.
package events

import "time"

// Subjects the consumers read and the delivery workers write.
const (
	ActivitySubject = "user-activity"
	FollowSubject   = "user-follow"
	DeliverSubject  = "notification-deliver"
)

// Activity is what list-service publishes on ActivitySubject.
type Activity struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	UserID         string    `json:"user_id"`
	AnimeID        *string   `json:"anime_id,omitempty"`
	WorkID         *string   `json:"work_id,omitempty"`
	Status         *string   `json:"status,omitempty"`
	PreviousStatus *string   `json:"previous_status,omitempty"`
	Score          *float64  `json:"score,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// Activity types list-service emits. Removals are stored for the actor's own
// history but never fanned out.
const (
	AnimeAdded         = "anime.added"
	AnimeStatusChanged = "anime.status_changed"
	AnimeScored        = "anime.scored"
	AnimeRemoved       = "anime.removed"
	WorkAdded          = "work.added"
	WorkStatusChanged  = "work.status_changed"
	WorkScored         = "work.scored"
	WorkRemoved        = "work.removed"
)

// Follow is what user-service publishes on FollowSubject.
type Follow struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	FollowerID     string    `json:"follower_id"`
	FolloweeID     string    `json:"followee_id"`
	AutoAccepted   bool      `json:"auto_accepted,omitempty"`
	PreviousStatus string    `json:"previous_status,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// Follow event types user-service emits.
const (
	FollowRequested = "follow_requested"
	FollowAccepted  = "follow_accepted"
	FollowDeclined  = "follow_declined"
	Unfollowed      = "unfollowed"
	FollowerRemoved = "follower_removed"
)

// Delivery is what this service publishes on DeliverSubject for the push and
// email workers. Channels is the subset the recipient has enabled.
type Delivery struct {
	NotificationID string   `json:"notification_id"`
	UserID         string   `json:"user_id"`
	Type           string   `json:"type"`
	Channels       []string `json:"channels"`
}
