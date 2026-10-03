//go:build integration

package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/weeb-vip/notifications-service/internal/events"
)

func TestFollowEventsBecomeNotifications(t *testing.T) {
	alice, bob := newUser(t, "alice"), newUser(t, "bob")
	users.setPublic(bob, true)

	publish(t, events.FollowSubject, followEvent(events.FollowAccepted, alice, bob, true))

	eventually(t, "bob gets a NEW_FOLLOWER notification", func() bool { return unreadCount(t, bob) == 1 })
	page := inbox(t, bob, false)
	require.Len(t, page.Notifications, 1)
	n := page.Notifications[0]
	assert.Equal(t, "NEW_FOLLOWER", n.Type)
	require.NotNil(t, n.Actor)
	assert.Equal(t, alice, n.Actor.ID)
	require.NotNil(t, n.Payload)
	assert.Contains(t, *n.Payload, alice)
	assert.Nil(t, n.ReadAt)
	assert.Equal(t, 0, unreadCount(t, alice), "an auto-accepted follow tells nobody on alice's side")

	var mark struct{ MarkAllNotificationsRead bool }
	mustData(t, bob, `mutation { markAllNotificationsRead }`, nil, &mark)
	assert.True(t, mark.MarkAllNotificationsRead)
	assert.Equal(t, 0, unreadCount(t, bob))
	assert.Len(t, inbox(t, bob, true).Notifications, 0)
	assert.Len(t, inbox(t, bob, false).Notifications, 1, "read ones are still listed")
	assert.NotNil(t, inbox(t, bob, false).Notifications[0].ReadAt)

	// Carol asks to follow bob (who requires approval); bob approves.
	carol := newUser(t, "carol")
	publish(t, events.FollowSubject, followEvent(events.FollowRequested, carol, bob, false))
	eventually(t, "bob sees the request", func() bool { return unreadCount(t, bob) == 1 })
	assert.Equal(t, "FOLLOW_REQUESTED", inbox(t, bob, true).Notifications[0].Type)

	publish(t, events.FollowSubject, followEvent(events.FollowAccepted, carol, bob, false))
	eventually(t, "carol learns she was accepted", func() bool { return unreadCount(t, carol) == 1 })
	assert.Equal(t, "FOLLOW_ACCEPTED", inbox(t, carol, true).Notifications[0].Type)
	assert.Equal(t, bob, inbox(t, carol, true).Notifications[0].Actor.ID)
	eventually(t, "bob also gets NEW_FOLLOWER", func() bool { return unreadCount(t, bob) == 2 })

	// Only the owner reads an inbox: ids that are not bob's are ignored.
	carolID := inbox(t, carol, true).Notifications[0].ID
	var one struct{ MarkNotificationsRead bool }
	mustData(t, bob, `mutation($ids: [ID!]!) { markNotificationsRead(ids: $ids) }`, map[string]any{"ids": []string{carolID}}, &one)
	assert.Equal(t, 1, unreadCount(t, carol), "bob cannot mark carol's notification")

	resp := query(t, "", `query { unreadNotificationCount }`, nil)
	assert.NotEmpty(t, resp.Errors, "the inbox needs a signed-in viewer")
}

func TestActivityFansOutToFollowers(t *testing.T) {
	bob, alice, carol, dave := newUser(t, "bob"), newUser(t, "alice"), newUser(t, "carol"), newUser(t, "dave")
	users.setPublic(bob, true)
	users.setFollowers(bob, alice, carol)

	ev := activityEvent(bob, events.AnimeStatusChanged)
	ev["previous_status"] = "PLANTOWATCH"
	publish(t, events.ActivitySubject, ev)

	eventually(t, "alice's feed has it", func() bool { return feed(t, alice).Total == 1 })
	eventually(t, "carol's feed has it", func() bool { return feed(t, carol).Total == 1 })
	assert.Equal(t, 0, feed(t, dave).Total, "non-followers see nothing")

	item := feed(t, alice).Activities[0]
	assert.Equal(t, ev["id"], item.ID)
	assert.Equal(t, "ANIME_STATUS_CHANGED", item.Type)
	assert.Equal(t, bob, item.Actor.ID)
	require.NotNil(t, item.Anime)
	assert.Equal(t, ev["anime_id"], item.Anime.ID)
	assert.Equal(t, "WATCHING", *item.Status)
	assert.Equal(t, "PLANTOWATCH", *item.PreviousStatus)

	// The actor's own public history lists it too.
	history := userActivity(t, bob)
	assert.Equal(t, 1, history.Total)

	// The same event again changes nothing.
	publish(t, events.ActivitySubject, ev)
	publish(t, events.ActivitySubject, activityEvent(bob, events.AnimeAdded))
	eventually(t, "second distinct event arrives", func() bool { return feed(t, alice).Total == 2 })
	assert.Equal(t, 2, feed(t, carol).Total, "the duplicate did not add a third")

	// Newest first.
	assert.Equal(t, "ANIME_ADDED", feed(t, alice).Activities[0].Type)

	// Removals are not shown.
	publish(t, events.ActivitySubject, activityEvent(bob, events.AnimeRemoved))
	publish(t, events.ActivitySubject, activityEvent(bob, events.AnimeScored))
	eventually(t, "scored arrives", func() bool { return feed(t, alice).Total == 3 })
	for _, a := range feed(t, alice).Activities {
		assert.NotEqual(t, "anime.removed", a.Type)
	}
	assert.Equal(t, 3, userActivity(t, bob).Total, "history hides removals as well")
}

func TestPrivateListsAreNotFannedOut(t *testing.T) {
	bob, alice := newUser(t, "bob"), newUser(t, "alice")
	users.setPublic(bob, false)
	users.setFollowers(bob, alice)

	ev := activityEvent(bob, events.AnimeAdded)
	publish(t, events.ActivitySubject, ev)

	eventually(t, "the activity is stored", func() bool {
		var n int64
		database.Raw("SELECT count(*) FROM activities WHERE event_id = ?", ev["id"]).Scan(&n)

		return n == 1
	})
	assert.Equal(t, 0, feed(t, alice).Total)
	assert.Equal(t, 0, userActivity(t, bob).Total, "a private user's history is hidden")

	users.setPublic(bob, true)
	assert.Equal(t, 1, userActivity(t, bob).Total, "making lists public reveals the stored history")
}

func TestUnfollowClearsTheFeedAndASeedFillsIt(t *testing.T) {
	bob, alice := newUser(t, "bob"), newUser(t, "alice")
	users.setPublic(bob, true)
	users.setFollowers(bob)

	// Bob was active before alice followed.
	publish(t, events.ActivitySubject, activityEvent(bob, events.AnimeAdded))
	publish(t, events.ActivitySubject, activityEvent(bob, events.WorkAdded))
	eventually(t, "bob's history has two", func() bool { return userActivity(t, bob).Total == 2 })
	assert.Equal(t, 0, feed(t, alice).Total)

	// Following seeds her feed with his recent activity.
	users.setFollowers(bob, alice)
	publish(t, events.FollowSubject, followEvent(events.FollowAccepted, alice, bob, true))
	eventually(t, "alice's feed is seeded", func() bool { return feed(t, alice).Total == 2 })

	publish(t, events.FollowSubject, followEvent(events.Unfollowed, alice, bob, false))
	eventually(t, "alice's feed is cleared", func() bool { return feed(t, alice).Total == 0 })
	assert.Equal(t, 2, userActivity(t, bob).Total, "bob's own history is untouched")
}

func TestWithdrawnRequestRemovesTheNotification(t *testing.T) {
	bob, alice := newUser(t, "bob"), newUser(t, "alice")
	publish(t, events.FollowSubject, followEvent(events.FollowRequested, alice, bob, false))
	eventually(t, "bob sees the request", func() bool { return unreadCount(t, bob) == 1 })

	publish(t, events.FollowSubject, followEvent(events.Unfollowed, alice, bob, false))
	eventually(t, "the request notification is gone", func() bool { return unreadCount(t, bob) == 0 })
}

func TestPreferencesDriveDelivery(t *testing.T) {
	bob, alice := newUser(t, "bob"), newUser(t, "alice")

	var prefs struct {
		NotificationPreferences []struct {
			Type, Channel string
			Enabled       bool
		}
	}
	mustData(t, bob, `query { notificationPreferences { type channel enabled } }`, nil, &prefs)
	assert.Len(t, prefs.NotificationPreferences, 9)
	for _, p := range prefs.NotificationPreferences {
		assert.Equal(t, p.Channel == "IN_APP", p.Enabled, "%s/%s default", p.Type, p.Channel)
	}

	before := streamMessages(t, events.DeliverSubject)
	var upd struct {
		UpdateNotificationPreference struct {
			Type, Channel string
			Enabled       bool
		}
	}
	mustData(t, bob, `mutation { updateNotificationPreference(type: FOLLOW_REQUESTED, channel: EMAIL, enabled: true) { type channel enabled } }`, nil, &upd)
	assert.True(t, upd.UpdateNotificationPreference.Enabled)

	publish(t, events.FollowSubject, followEvent(events.FollowRequested, alice, bob, false))
	eventually(t, "bob is notified", func() bool { return unreadCount(t, bob) == 1 })
	eventually(t, "a delivery was requested", func() bool { return streamMessages(t, events.DeliverSubject) == before+1 })
	delivery := lastDelivery(t)
	require.NotNil(t, delivery)
	assert.Equal(t, bob, delivery.UserID)
	assert.Equal(t, "follow_requested", delivery.Type)
	assert.Equal(t, []string{"email"}, delivery.Channels)
	assert.Equal(t, inbox(t, bob, true).Notifications[0].ID, delivery.NotificationID)

	resp := query(t, "", `query { notificationPreferences { type } }`, nil)
	assert.NotEmpty(t, resp.Errors)
}

func TestPushSubscriptions(t *testing.T) {
	bob := newUser(t, "bob")
	endpoint := "https://push.example/" + bob

	var reg struct {
		RegisterPushSubscription struct{ ID, Platform, Endpoint string }
	}
	mustData(t, bob, `mutation($input: PushSubscriptionInput!) { registerPushSubscription(input: $input) { id platform endpoint } }`,
		map[string]any{"input": map[string]any{"platform": "WEB", "endpoint": endpoint, "p256dh": "k", "auth": "a"}}, &reg)
	assert.Equal(t, "WEB", reg.RegisterPushSubscription.Platform)
	first := reg.RegisterPushSubscription.ID

	// Re-registering the same endpoint updates rather than duplicates.
	mustData(t, bob, `mutation($input: PushSubscriptionInput!) { registerPushSubscription(input: $input) { id platform endpoint } }`,
		map[string]any{"input": map[string]any{"platform": "WEB", "endpoint": endpoint, "p256dh": "k2", "auth": "a2"}}, &reg)
	assert.Equal(t, first, reg.RegisterPushSubscription.ID)

	var list struct{ PushSubscriptions []struct{ ID string } }
	mustData(t, bob, `query { pushSubscriptions { id } }`, nil, &list)
	assert.Len(t, list.PushSubscriptions, 1)

	var un struct{ UnregisterPushSubscription bool }
	mustData(t, bob, `mutation($e: String!) { unregisterPushSubscription(endpoint: $e) }`, map[string]any{"e": endpoint}, &un)
	assert.True(t, un.UnregisterPushSubscription)
	mustData(t, bob, `query { pushSubscriptions { id } }`, nil, &list)
	assert.Empty(t, list.PushSubscriptions)
}

func TestMalformedEventsReachTheDeadLetterSubject(t *testing.T) {
	before := streamMessages(t, events.ActivitySubject+"-dlq")
	publish(t, events.ActivitySubject, map[string]any{"id": "not-a-uuid", "type": events.AnimeAdded, "user_id": "user_x"})
	eventually(t, "the event lands on the dead-letter subject after retries", func() bool {
		return streamMessages(t, events.ActivitySubject+"-dlq") == before+1
	})
}
