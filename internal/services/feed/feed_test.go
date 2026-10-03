package feed_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/weeb-vip/notifications-service/internal/db/repositories/activities"
	"github.com/weeb-vip/notifications-service/internal/events"
	"github.com/weeb-vip/notifications-service/internal/services/feed"
)

type fakeRepo struct {
	activities.Repository
	stored    map[string]*activities.Activity
	fanouts   map[string][]string // activity id -> recipients
	removed   [][2]string
	recent    []*activities.Activity
	insertErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{stored: map[string]*activities.Activity{}, fanouts: map[string][]string{}}
}

func (f *fakeRepo) Insert(_ context.Context, a *activities.Activity) (bool, error) {
	if f.insertErr != nil {
		return false, f.insertErr
	}
	if _, dup := f.stored[a.EventID]; dup {
		return false, nil
	}
	f.stored[a.EventID] = a

	return true, nil
}

func (f *fakeRepo) FanOut(_ context.Context, a *activities.Activity, ids []string) error {
	f.fanouts[a.ID] = append(f.fanouts[a.ID], ids...)

	return nil
}

func (f *fakeRepo) RecentByActor(_ context.Context, _ string, _ int) ([]*activities.Activity, error) {
	return f.recent, nil
}

func (f *fakeRepo) RemoveActorFromFeed(_ context.Context, userID, actorID string) (int64, error) {
	f.removed = append(f.removed, [2]string{userID, actorID})

	return 1, nil
}

func (f *fakeRepo) ByActor(_ context.Context, actorID string, _, _ int) ([]*activities.Activity, int64, error) {
	var out []*activities.Activity
	for _, a := range f.stored {
		if a.ActorID == actorID {
			out = append(out, a)
		}
	}

	return out, int64(len(out)), nil
}

type fakeUsers struct {
	public    map[string]bool
	followers map[string][]string
	calls     int
	err       error
}

func (u *fakeUsers) ListsPublic(_ context.Context, id string) (bool, error) {
	if u.err != nil {
		return false, u.err
	}

	return u.public[id], nil
}

func (u *fakeUsers) FollowerIDs(_ context.Context, id, after string, limit int) ([]string, error) {
	u.calls++
	all := u.followers[id]
	start := 0
	if after != "" {
		for i, f := range all {
			if f == after {
				start = i + 1
			}
		}
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	if start >= len(all) {
		return []string{}, nil
	}

	return all[start:end], nil
}

func activity(actor, typ string) events.Activity {
	anime := "anime-1"

	return events.Activity{ID: uuid.NewString(), Type: typ, UserID: actor, AnimeID: &anime, OccurredAt: time.Now()}
}

func TestIngest(t *testing.T) {
	t.Run("fans out to every follower in pages", func(t *testing.T) {
		repo := newFakeRepo()
		followers := make([]string, 0, 7)
		for i := 0; i < 7; i++ {
			followers = append(followers, "user_f"+string(rune('a'+i)))
		}
		users := &fakeUsers{public: map[string]bool{"user_b": true}, followers: map[string][]string{"user_b": followers}}
		svc := feed.New(repo, users, feed.Config{FollowerPage: 3}, zerolog.Nop())

		ev := activity("user_b", events.AnimeStatusChanged)
		out, err := svc.Ingest(context.Background(), ev)
		require.NoError(t, err)
		assert.True(t, out.Stored)
		assert.Equal(t, 7, out.FannedOut)
		assert.ElementsMatch(t, followers, repo.fanouts[ev.ID])
		assert.Equal(t, 3, users.calls, "7 followers in pages of 3 is three calls")
		assert.Equal(t, ev.ID, repo.stored[ev.ID].ID, "activity id is the event id")
	})

	t.Run("duplicate event is a no-op", func(t *testing.T) {
		repo := newFakeRepo()
		users := &fakeUsers{public: map[string]bool{"user_b": true}, followers: map[string][]string{"user_b": {"user_a"}}}
		svc := feed.New(repo, users, feed.Config{}, zerolog.Nop())

		ev := activity("user_b", events.AnimeAdded)
		_, err := svc.Ingest(context.Background(), ev)
		require.NoError(t, err)
		out, err := svc.Ingest(context.Background(), ev)
		require.NoError(t, err)
		assert.False(t, out.Stored)
		assert.Equal(t, "duplicate", out.SkipReason)
		assert.Len(t, repo.fanouts[ev.ID], 1)
	})

	t.Run("private lists are stored but not fanned out", func(t *testing.T) {
		repo := newFakeRepo()
		users := &fakeUsers{public: map[string]bool{}, followers: map[string][]string{"user_b": {"user_a"}}}
		svc := feed.New(repo, users, feed.Config{}, zerolog.Nop())

		ev := activity("user_b", events.AnimeAdded)
		out, err := svc.Ingest(context.Background(), ev)
		require.NoError(t, err)
		assert.True(t, out.Stored)
		assert.Equal(t, "lists private", out.SkipReason)
		assert.Empty(t, repo.fanouts)
		assert.Equal(t, 0, users.calls, "followers are never asked for")
	})

	t.Run("removals are stored but not fanned out", func(t *testing.T) {
		repo := newFakeRepo()
		users := &fakeUsers{public: map[string]bool{"user_b": true}, followers: map[string][]string{"user_b": {"user_a"}}}
		svc := feed.New(repo, users, feed.Config{}, zerolog.Nop())

		out, err := svc.Ingest(context.Background(), activity("user_b", events.AnimeRemoved))
		require.NoError(t, err)
		assert.True(t, out.Stored)
		assert.Empty(t, repo.fanouts)
	})

	t.Run("malformed events are errors so they reach the dead-letter subject", func(t *testing.T) {
		svc := feed.New(newFakeRepo(), &fakeUsers{}, feed.Config{}, zerolog.Nop())
		_, err := svc.Ingest(context.Background(), events.Activity{ID: "not-a-uuid", Type: events.AnimeAdded, UserID: "user_b"})
		assert.Error(t, err)
		_, err = svc.Ingest(context.Background(), events.Activity{ID: uuid.NewString(), Type: events.AnimeAdded})
		assert.Error(t, err)
	})

	t.Run("user-service failure is returned for retry", func(t *testing.T) {
		repo := newFakeRepo()
		users := &fakeUsers{err: errors.New("user-service down")}
		svc := feed.New(repo, users, feed.Config{}, zerolog.Nop())

		_, err := svc.Ingest(context.Background(), activity("user_b", events.AnimeAdded))
		assert.Error(t, err)
	})
}

func TestUserActivityRespectsPrivacy(t *testing.T) {
	repo := newFakeRepo()
	a := &activities.Activity{ID: uuid.NewString(), EventID: uuid.NewString(), ActorID: "user_b", Type: events.AnimeAdded}
	repo.stored[a.EventID] = a
	users := &fakeUsers{public: map[string]bool{"user_b": false}}
	svc := feed.New(repo, users, feed.Config{}, zerolog.Nop())

	items, total, err := svc.UserActivity(context.Background(), "user_b", 1, 10)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.EqualValues(t, 0, total)

	users.public["user_b"] = true
	items, total, err = svc.UserActivity(context.Background(), "user_b", 1, 10)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.EqualValues(t, 1, total)
}

func TestSeedAndUnfollow(t *testing.T) {
	repo := newFakeRepo()
	a1 := &activities.Activity{ID: uuid.NewString(), ActorID: "user_b", Type: events.AnimeAdded}
	a2 := &activities.Activity{ID: uuid.NewString(), ActorID: "user_b", Type: events.WorkAdded}
	repo.recent = []*activities.Activity{a1, a2}
	users := &fakeUsers{public: map[string]bool{"user_b": true}}
	svc := feed.New(repo, users, feed.Config{}, zerolog.Nop())

	require.NoError(t, svc.Seed(context.Background(), "user_a", "user_b"))
	assert.Equal(t, []string{"user_a"}, repo.fanouts[a1.ID])
	assert.Equal(t, []string{"user_a"}, repo.fanouts[a2.ID])

	users.public["user_b"] = false
	repo.fanouts = map[string][]string{}
	require.NoError(t, svc.Seed(context.Background(), "user_c", "user_b"))
	assert.Empty(t, repo.fanouts, "a private actor seeds nothing")

	require.NoError(t, svc.Unfollow(context.Background(), "user_a", "user_b"))
	assert.Equal(t, [][2]string{{"user_a", "user_b"}}, repo.removed)
}
