// Package feed turns activity events into per-user feeds.
package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/weeb-vip/notifications-service/internal/clients/userservice"
	"github.com/weeb-vip/notifications-service/internal/db/repositories/activities"
	"github.com/weeb-vip/notifications-service/internal/events"
)

// Service is the feed's behaviour.
type Service interface {
	// Ingest stores an activity and fans it out to the actor's followers.
	// Redelivered events are no-ops; actors whose lists are private are
	// stored but not fanned out; removals are stored but not fanned out.
	Ingest(ctx context.Context, event events.Activity) (Outcome, error)
	// Feed pages the viewer's feed.
	Feed(ctx context.Context, userID string, page, limit int) ([]*activities.Activity, int64, error)
	// UserActivity pages one user's own activity, empty unless their lists
	// are public.
	UserActivity(ctx context.Context, userID string, page, limit int) ([]*activities.Activity, int64, error)
	// Seed copies the actor's recent activity into a new follower's feed.
	Seed(ctx context.Context, followerID, actorID string) error
	// Unfollow removes everything by the actor from the follower's feed.
	Unfollow(ctx context.Context, followerID, actorID string) error
	// Activity returns one activity by id, nil when there is none.
	Activity(ctx context.Context, id string) (*activities.Activity, error)
	Prune(ctx context.Context, olderThan time.Duration) (int64, error)
}

// Outcome says what Ingest did, for logs and tests.
type Outcome struct {
	Stored     bool
	FannedOut  int
	SkipReason string
}

// Config tunes fan-out.
type Config struct {
	// FollowerPage is how many follower ids are fetched per call. Default 500.
	FollowerPage int
	// SeedLimit is how many recent activities a new follower receives. Default 20.
	SeedLimit int
}

func (c Config) withDefaults() Config {
	if c.FollowerPage <= 0 {
		c.FollowerPage = 500
	}
	if c.SeedLimit <= 0 {
		c.SeedLimit = 20
	}

	return c
}

type service struct {
	repo  activities.Repository
	users userservice.Client
	cfg   Config
	log   zerolog.Logger
}

// New wires the service.
func New(repo activities.Repository, users userservice.Client, cfg Config, log zerolog.Logger) Service {
	return &service{repo: repo, users: users, cfg: cfg.withDefaults(), log: log}
}

// fanOutTypes are the events a feed shows. Removals are kept for the actor's
// own history only.
var fanOutTypes = map[string]bool{
	events.AnimeAdded: true, events.AnimeStatusChanged: true, events.AnimeScored: true,
	events.WorkAdded: true, events.WorkStatusChanged: true, events.WorkScored: true,
}

func fromEvent(event events.Activity) (*activities.Activity, error) {
	eventID, err := uuid.Parse(event.ID)
	if err != nil {
		return nil, fmt.Errorf("feed: event id %q is not a uuid: %w", event.ID, err)
	}
	if event.UserID == "" || event.Type == "" {
		return nil, fmt.Errorf("feed: event %s missing user_id or type", event.ID)
	}
	occurred := event.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}

	return &activities.Activity{
		// The activity id is the event id: one event, one activity, and the
		// row a redelivery would conflict with is the one it would create.
		ID:             eventID.String(),
		EventID:        eventID.String(),
		ActorID:        event.UserID,
		Type:           event.Type,
		AnimeID:        event.AnimeID,
		WorkID:         event.WorkID,
		Status:         event.Status,
		PreviousStatus: event.PreviousStatus,
		Score:          event.Score,
		OccurredAt:     occurred,
	}, nil
}

func (s *service) Ingest(ctx context.Context, event events.Activity) (Outcome, error) {
	activity, err := fromEvent(event)
	if err != nil {
		return Outcome{}, err
	}

	stored, err := s.repo.Insert(ctx, activity)
	if err != nil {
		return Outcome{}, fmt.Errorf("feed: store activity: %w", err)
	}
	if !stored {
		return Outcome{SkipReason: "duplicate"}, nil
	}
	if !fanOutTypes[activity.Type] {
		return Outcome{Stored: true, SkipReason: "type not shown"}, nil
	}

	public, err := s.users.ListsPublic(ctx, activity.ActorID)
	if err != nil {
		// The activity is stored; the fan-out is what failed. Returning the
		// error has the consumer retry the whole event, and the duplicate
		// path above then skips straight to this point again -- but only
		// once Insert reports not-new, which means the fan-out below must
		// also be reachable on a retry. It is: see the duplicate handling in
		// the consumer, which calls FanOutStored.
		return Outcome{Stored: true}, fmt.Errorf("feed: lists_public for %s: %w", activity.ActorID, err)
	}
	if !public {
		return Outcome{Stored: true, SkipReason: "lists private"}, nil
	}

	n, err := s.fanOut(ctx, activity)

	return Outcome{Stored: true, FannedOut: n}, err
}

// fanOut pages the actor's followers and writes their feed rows.
func (s *service) fanOut(ctx context.Context, activity *activities.Activity) (int, error) {
	total := 0
	after := ""
	for {
		ids, err := s.users.FollowerIDs(ctx, activity.ActorID, after, s.cfg.FollowerPage)
		if err != nil {
			return total, fmt.Errorf("feed: followers of %s: %w", activity.ActorID, err)
		}
		if len(ids) == 0 {
			return total, nil
		}
		if err := s.repo.FanOut(ctx, activity, ids); err != nil {
			return total, fmt.Errorf("feed: fan out %s: %w", activity.ID, err)
		}
		total += len(ids)
		if len(ids) < s.cfg.FollowerPage {
			return total, nil
		}
		after = ids[len(ids)-1]
	}
}

func (s *service) Feed(ctx context.Context, userID string, page, limit int) ([]*activities.Activity, int64, error) {
	items, total, err := s.repo.Feed(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []*activities.Activity{}
	}

	return items, total, nil
}

func (s *service) UserActivity(ctx context.Context, userID string, page, limit int) ([]*activities.Activity, int64, error) {
	public, err := s.users.ListsPublic(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	if !public {
		return []*activities.Activity{}, 0, nil
	}
	items, total, err := s.repo.ByActor(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []*activities.Activity{}
	}

	return items, total, nil
}

func (s *service) Seed(ctx context.Context, followerID, actorID string) error {
	public, err := s.users.ListsPublic(ctx, actorID)
	if err != nil {
		return err
	}
	if !public {
		return nil
	}
	recent, err := s.repo.RecentByActor(ctx, actorID, s.cfg.SeedLimit)
	if err != nil {
		return err
	}
	for _, activity := range recent {
		if err := s.repo.FanOut(ctx, activity, []string{followerID}); err != nil {
			return err
		}
	}

	return nil
}

func (s *service) Unfollow(ctx context.Context, followerID, actorID string) error {
	_, err := s.repo.RemoveActorFromFeed(ctx, followerID, actorID)

	return err
}

func (s *service) Activity(ctx context.Context, id string) (*activities.Activity, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *service) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.repo.Prune(ctx, time.Now().UTC().Add(-olderThan))
}
