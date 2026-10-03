//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	epnats "github.com/ThatCatDev/ep/v2/drivers/nats"
	"github.com/google/uuid"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/weeb-vip/notifications-service/internal/clients/userservice"
	"github.com/weeb-vip/notifications-service/internal/events"
)

func userServiceClient(url string) userservice.Client { return userservice.New(url) }

type gqlResponse struct {
	Data   json.RawMessage   `json:"data"`
	Errors []json.RawMessage `json:"errors"`
}

func query(t *testing.T, userID, operation string, variables map[string]any) *gqlResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": operation, "variables": variables})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, api.URL+"/graphql", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("x-user-id", userID)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var out gqlResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))

	return &out
}

func mustData(t *testing.T, userID, operation string, variables map[string]any, into any) {
	t.Helper()
	resp := query(t, userID, operation, variables)
	require.Empty(t, resp.Errors, "unexpected GraphQL errors: %s", resp.Errors)
	if into != nil {
		require.NoError(t, json.Unmarshal(resp.Data, into))
	}
}

func publish(t *testing.T, subject string, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	id := uuid.NewString()
	if m, ok := payload.(map[string]any); ok {
		if s, ok := m["id"].(string); ok {
			id = s
		}
	}
	require.NoError(t, producer.Produce(context.Background(), subject, &epnats.Message{
		Subject: subject, Data: body, Headers: map[string]string{"Nats-Msg-Id": id},
	}))
}

func activityEvent(actor, typ string) map[string]any {
	return map[string]any{
		"id": uuid.NewString(), "type": typ, "user_id": actor, "anime_id": uuid.NewString(),
		"status": "WATCHING", "occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func followEvent(typ, follower, followee string, auto bool) map[string]any {
	return map[string]any{
		"id": uuid.NewString(), "type": typ, "follower_id": follower, "followee_id": followee,
		"auto_accepted": auto, "occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func newUser(t *testing.T, label string) string {
	id := fmt.Sprintf("user_%s_%d", label, time.Now().UnixNano())
	t.Cleanup(func() {
		database.Exec("DELETE FROM feed_items WHERE user_id = ? OR actor_id = ?", id, id)
		database.Exec("DELETE FROM notifications WHERE user_id = ? OR actor_id = ?", id, id)
		database.Exec("DELETE FROM notification_preferences WHERE user_id = ?", id)
		database.Exec("DELETE FROM push_subscriptions WHERE user_id = ?", id)
		database.Exec("DELETE FROM activities WHERE actor_id = ?", id)
	})

	return id
}

type activity struct {
	ID             string               `json:"id"`
	Type           string               `json:"type"`
	Actor          struct{ ID string }  `json:"actor"`
	Anime          *struct{ ID string } `json:"anime"`
	Status         *string              `json:"status"`
	PreviousStatus *string              `json:"previousStatus"`
	OccurredAt     string               `json:"occurredAt"`
}

type activityPage struct {
	Total      int        `json:"total"`
	Activities []activity `json:"activities"`
}

func feed(t *testing.T, userID string) activityPage {
	t.Helper()
	var out struct{ Feed activityPage }
	mustData(t, userID, `query { feed(page: 1, limit: 50) { total activities { id type actor { id } anime { id } status previousStatus occurredAt } } }`, nil, &out)

	return out.Feed
}

func userActivity(t *testing.T, userID string) activityPage {
	t.Helper()
	var out struct{ UserActivity activityPage }
	mustData(t, "", `query($id: ID!) { userActivity(userID: $id, page: 1, limit: 50) { total activities { id type actor { id } } } }`, map[string]any{"id": userID}, &out)

	return out.UserActivity
}

type notification struct {
	ID      string               `json:"id"`
	Type    string               `json:"type"`
	Actor   *struct{ ID string } `json:"actor"`
	Payload *string              `json:"payload"`
	ReadAt  *string              `json:"readAt"`
}

type notificationPage struct {
	Total         int            `json:"total"`
	Notifications []notification `json:"notifications"`
}

func inbox(t *testing.T, userID string, unreadOnly bool) notificationPage {
	t.Helper()
	var out struct{ Notifications notificationPage }
	mustData(t, userID, `query($unread: Boolean) { notifications(page: 1, limit: 50, unreadOnly: $unread) { total notifications { id type actor { id } payload readAt } } }`,
		map[string]any{"unread": unreadOnly}, &out)

	return out.Notifications
}

func unreadCount(t *testing.T, userID string) int {
	t.Helper()
	var out struct{ UnreadNotificationCount int }
	mustData(t, userID, `query { unreadNotificationCount }`, nil, &out)

	return out.UnreadNotificationCount
}

// eventually polls until the condition holds; consumers are asynchronous.
func eventually(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 10*time.Second, 50*time.Millisecond, msg)
}

// streamMessages counts the messages on a subject's stream, for asserting on
// what reached the delivery, retry and dead-letter subjects.
func streamMessages(t *testing.T, subject string) uint64 {
	t.Helper()
	nc, err := natsgo.Connect(nats.ClientURL())
	require.NoError(t, err)
	defer nc.Close()
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	stream, err := js.Stream(context.Background(), epnats.StreamNameForSubject(subject))
	if err != nil {
		return 0
	}
	info, err := stream.Info(context.Background())
	require.NoError(t, err)

	return info.State.Msgs
}

// lastDelivery reads the newest message on the delivery subject.
func lastDelivery(t *testing.T) *events.Delivery {
	t.Helper()
	nc, err := natsgo.Connect(nats.ClientURL())
	require.NoError(t, err)
	defer nc.Close()
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	ctx := context.Background()
	stream, err := js.Stream(ctx, epnats.StreamNameForSubject(events.DeliverSubject))
	if err != nil {
		return nil
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{DeliverPolicy: jetstream.DeliverLastPolicy, AckPolicy: jetstream.AckNonePolicy})
	require.NoError(t, err)
	msg, err := consumer.Next(jetstream.FetchMaxWait(2 * time.Second))
	if err != nil {
		return nil
	}
	var delivery events.Delivery
	require.NoError(t, json.Unmarshal(msg.Data(), &delivery))

	return &delivery
}
