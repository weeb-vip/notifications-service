package notifications_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	repo "github.com/weeb-vip/notifications-service/internal/db/repositories/notifications"
	"github.com/weeb-vip/notifications-service/internal/events"
	"github.com/weeb-vip/notifications-service/internal/services/notifications"
)

type fakeRepo struct {
	repo.Repository
	rows    []*repo.Notification
	prefs   []*repo.Preference
	deleted [][3]string
}

func (f *fakeRepo) Insert(_ context.Context, n *repo.Notification) (bool, error) {
	for _, existing := range f.rows {
		if existing.UserID == n.UserID && existing.EventID == n.EventID {
			return false, nil
		}
	}
	f.rows = append(f.rows, n)

	return true, nil
}

func (f *fakeRepo) Preferences(_ context.Context, userID string) ([]*repo.Preference, error) {
	var out []*repo.Preference
	for _, p := range f.prefs {
		if p.UserID == userID {
			out = append(out, p)
		}
	}

	return out, nil
}

func (f *fakeRepo) UpsertPreference(_ context.Context, p *repo.Preference) error {
	for i, existing := range f.prefs {
		if existing.UserID == p.UserID && existing.Type == p.Type && existing.Channel == p.Channel {
			f.prefs[i] = p

			return nil
		}
	}
	f.prefs = append(f.prefs, p)

	return nil
}

func (f *fakeRepo) DeleteByActor(_ context.Context, userID, actorID string, types []string) (int64, error) {
	f.deleted = append(f.deleted, [3]string{userID, actorID, types[0]})

	return 1, nil
}

type fakeFeed struct {
	seeded     [][2]string
	unfollowed [][2]string
}

func (f *fakeFeed) Seed(_ context.Context, follower, actor string) error {
	f.seeded = append(f.seeded, [2]string{follower, actor})

	return nil
}

func (f *fakeFeed) Unfollow(_ context.Context, follower, actor string) error {
	f.unfollowed = append(f.unfollowed, [2]string{follower, actor})

	return nil
}

type fakeDeliver struct{ published []events.Delivery }

func (d *fakeDeliver) Publish(_ context.Context, delivery events.Delivery) error {
	d.published = append(d.published, delivery)

	return nil
}

type fixture struct {
	repo    *fakeRepo
	feed    *fakeFeed
	deliver *fakeDeliver
	svc     notifications.Service
}

func newFixture() *fixture {
	f := &fixture{repo: &fakeRepo{}, feed: &fakeFeed{}, deliver: &fakeDeliver{}}
	f.svc = notifications.New(f.repo, f.feed, f.deliver, zerolog.Nop())

	return f
}

func follow(typ string, auto bool) events.Follow {
	return events.Follow{ID: uuid.NewString(), Type: typ, FollowerID: "user_alice", FolloweeID: "user_bob", AutoAccepted: auto, OccurredAt: time.Now()}
}

func (f *fixture) types(userID string) []string {
	var out []string
	for _, n := range f.repo.rows {
		if n.UserID == userID {
			out = append(out, n.Type)
		}
	}

	return out
}

func TestHandleFollow(t *testing.T) {
	t.Run("request notifies the followee", func(t *testing.T) {
		f := newFixture()
		require.NoError(t, f.svc.HandleFollow(context.Background(), follow(events.FollowRequested, false)))
		assert.Equal(t, []string{notifications.TypeFollowRequested}, f.types("user_bob"))
		assert.Empty(t, f.types("user_alice"))
		assert.Equal(t, "user_alice", *f.repo.rows[0].ActorID)
		assert.Empty(t, f.feed.seeded)
	})

	t.Run("auto-accepted follow notifies the followee and seeds the follower's feed", func(t *testing.T) {
		f := newFixture()
		require.NoError(t, f.svc.HandleFollow(context.Background(), follow(events.FollowAccepted, true)))
		assert.Equal(t, []string{notifications.TypeNewFollower}, f.types("user_bob"))
		assert.Empty(t, f.types("user_alice"), "nobody approved anything")
		assert.Equal(t, [][2]string{{"user_alice", "user_bob"}}, f.feed.seeded)
	})

	t.Run("approved request notifies both sides", func(t *testing.T) {
		f := newFixture()
		require.NoError(t, f.svc.HandleFollow(context.Background(), follow(events.FollowAccepted, false)))
		assert.Equal(t, []string{notifications.TypeFollowAccepted}, f.types("user_alice"))
		assert.Equal(t, []string{notifications.TypeNewFollower}, f.types("user_bob"))
		for _, n := range f.repo.rows {
			if n.Type == notifications.TypeFollowAccepted {
				assert.Equal(t, "user_bob", *n.ActorID, "the approver is the actor")
			} else {
				assert.Equal(t, "user_alice", *n.ActorID)
			}
		}
		assert.Len(t, f.feed.seeded, 1)
	})

	t.Run("redelivery creates nothing new", func(t *testing.T) {
		f := newFixture()
		ev := follow(events.FollowAccepted, false)
		require.NoError(t, f.svc.HandleFollow(context.Background(), ev))
		require.NoError(t, f.svc.HandleFollow(context.Background(), ev))
		assert.Len(t, f.repo.rows, 2)
	})

	t.Run("decline, unfollow and removal clear the feed and any pending request", func(t *testing.T) {
		for _, typ := range []string{events.FollowDeclined, events.Unfollowed, events.FollowerRemoved} {
			f := newFixture()
			require.NoError(t, f.svc.HandleFollow(context.Background(), follow(typ, false)))
			assert.Equal(t, [][2]string{{"user_alice", "user_bob"}}, f.feed.unfollowed, typ)
			assert.Equal(t, [][3]string{{"user_bob", "user_alice", notifications.TypeFollowRequested}}, f.repo.deleted, typ)
			assert.Empty(t, f.repo.rows, typ)
		}
	})

	t.Run("malformed events are errors", func(t *testing.T) {
		f := newFixture()
		assert.Error(t, f.svc.HandleFollow(context.Background(), events.Follow{ID: "nope", Type: events.FollowRequested, FollowerID: "a", FolloweeID: "b"}))
		assert.Error(t, f.svc.HandleFollow(context.Background(), events.Follow{ID: uuid.NewString(), Type: events.FollowRequested}))
	})

	t.Run("unknown types are ignored", func(t *testing.T) {
		f := newFixture()
		require.NoError(t, f.svc.HandleFollow(context.Background(), follow("something_new", false)))
		assert.Empty(t, f.repo.rows)
	})
}

func TestDeliveryFollowsPreferences(t *testing.T) {
	t.Run("defaults request no delivery", func(t *testing.T) {
		f := newFixture()
		require.NoError(t, f.svc.HandleFollow(context.Background(), follow(events.FollowRequested, false)))
		assert.Empty(t, f.deliver.published)
	})

	t.Run("enabled channels are requested, in-app never is", func(t *testing.T) {
		f := newFixture()
		_, err := f.svc.UpdatePreference(context.Background(), "user_bob", notifications.TypeFollowRequested, notifications.ChannelEmail, true)
		require.NoError(t, err)
		_, err = f.svc.UpdatePreference(context.Background(), "user_bob", notifications.TypeNewFollower, notifications.ChannelPush, true)
		require.NoError(t, err)

		require.NoError(t, f.svc.HandleFollow(context.Background(), follow(events.FollowRequested, false)))
		require.Len(t, f.deliver.published, 1)
		d := f.deliver.published[0]
		assert.Equal(t, "user_bob", d.UserID)
		assert.Equal(t, notifications.TypeFollowRequested, d.Type)
		assert.Equal(t, []string{notifications.ChannelEmail}, d.Channels)
		assert.Equal(t, f.repo.rows[0].ID, d.NotificationID)

		require.NoError(t, f.svc.HandleFollow(context.Background(), follow(events.FollowAccepted, true)))
		require.Len(t, f.deliver.published, 2)
		assert.Equal(t, []string{notifications.ChannelPush}, f.deliver.published[1].Channels)
	})

	t.Run("a disabled override wins over the default", func(t *testing.T) {
		f := newFixture()
		_, err := f.svc.UpdatePreference(context.Background(), "user_bob", notifications.TypeFollowRequested, notifications.ChannelPush, true)
		require.NoError(t, err)
		_, err = f.svc.UpdatePreference(context.Background(), "user_bob", notifications.TypeFollowRequested, notifications.ChannelPush, false)
		require.NoError(t, err)
		require.NoError(t, f.svc.HandleFollow(context.Background(), follow(events.FollowRequested, false)))
		assert.Empty(t, f.deliver.published)
	})
}

func TestPreferencesMatrix(t *testing.T) {
	f := newFixture()
	_, err := f.svc.UpdatePreference(context.Background(), "user_bob", notifications.TypeNewFollower, notifications.ChannelEmail, true)
	require.NoError(t, err)

	matrix, err := f.svc.Preferences(context.Background(), "user_bob")
	require.NoError(t, err)
	assert.Len(t, matrix, len(notifications.Types)*len(notifications.ChannelList))
	for _, p := range matrix {
		switch {
		case p.Channel == notifications.ChannelInApp:
			assert.True(t, p.Enabled, "in-app defaults on")
		case p.Type == notifications.TypeNewFollower && p.Channel == notifications.ChannelEmail:
			assert.True(t, p.Enabled, "the override")
		default:
			assert.False(t, p.Enabled, "%s/%s defaults off", p.Type, p.Channel)
		}
	}

	_, err = f.svc.UpdatePreference(context.Background(), "user_bob", "bogus", notifications.ChannelEmail, true)
	assert.Error(t, err)
	_, err = f.svc.UpdatePreference(context.Background(), "user_bob", notifications.TypeNewFollower, "fax", true)
	assert.Error(t, err)
}
