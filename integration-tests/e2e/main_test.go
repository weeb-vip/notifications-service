//go:build integration

// Package e2e drives notifications-service the way production does: events
// arrive on NATS JetStream, consumers write the database, the GraphQL API
// reads it. Everything runs in this process: an embedded NATS server, both
// consumers, the HTTP handler, and a fake user-service that answers the two
// queries the feed asks. Only Postgres is external.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	epnats "github.com/ThatCatDev/ep/v2/drivers/nats"
	"github.com/nats-io/nats-server/v2/server"
	"gorm.io/gorm"

	"github.com/weeb-vip/notifications-service/config"
	"github.com/weeb-vip/notifications-service/http/handlers"
	"github.com/weeb-vip/notifications-service/internal/consumers"
	"github.com/weeb-vip/notifications-service/internal/db"
	"github.com/weeb-vip/notifications-service/internal/logger"
	"github.com/weeb-vip/notifications-service/internal/wiring"
)

var (
	api      *httptest.Server
	database *gorm.DB
	nats     *server.Server
	producer *epnats.NatsDriver
	users    *fakeUserService
	cfg      config.Config
)

// fakeUserService answers publicUserByID and followerIDs from in-memory maps,
// so a test can decide who is public and who follows whom.
type fakeUserService struct {
	mu        sync.Mutex
	public    map[string]bool
	followers map[string][]string
	server    *httptest.Server
	calls     int
}

func newFakeUserService() *fakeUserService {
	f := &fakeUserService{public: map[string]bool{}, followers: map[string][]string{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))

	return f
}

func (f *fakeUserService) setPublic(userID string, public bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.public[userID] = public
}

func (f *fakeUserService) setFollowers(userID string, followers ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.followers[userID] = followers
}

func (f *fakeUserService) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	id, _ := req.Variables["id"].(string)
	w.Header().Set("Content-Type", "application/json")

	switch {
	case contains(req.Query, "publicUserByID"):
		public, known := f.public[id]
		if !known {
			fmt.Fprint(w, `{"data":{"publicUserByID":null}}`)

			return
		}
		fmt.Fprintf(w, `{"data":{"publicUserByID":{"listsPublic":%t}}}`, public)
	case contains(req.Query, "followerIDs"):
		after, _ := req.Variables["after"].(string)
		limit := int(req.Variables["limit"].(float64))
		all := f.followers[id]
		start := 0
		if after != "" {
			for i, v := range all {
				if v == after {
					start = i + 1
				}
			}
		}
		end := start + limit
		if end > len(all) {
			end = len(all)
		}
		page := []string{}
		if start < len(all) {
			page = all[start:end]
		}
		body, _ := json.Marshal(map[string]any{"data": map[string]any{"followerIDs": page}})
		w.Write(body)
	default:
		http.Error(w, `{"errors":[{"message":"unknown query"}]}`, http.StatusBadRequest)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool { return indexOf(s, sub) >= 0 })()
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}

	return -1
}

func TestMain(m *testing.M) {
	defaults := map[string]string{
		"DBHOST": "localhost", "DBPORT": "5432", "DBUSERNAME": "postgres", "DBPASSWORD": "postgres",
		"DBNAME": "weeb", "DBSSL": "disable", "DBMIGRATIONTABLE": "__migrations_notifications-service",
	}
	for k, v := range defaults {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
	logger.Logger(logger.WithServerName("notifications-service-test"), logger.WithVersion("test"), logger.WithEnvironment("test"))

	// Embedded JetStream, so no Docker and no shared state between runs.
	var err error
	nats, err = server.NewServer(&server.Options{Port: -1, JetStream: true, StoreDir: os.TempDir() + fmt.Sprintf("/ns-e2e-%d", time.Now().UnixNano()), NoLog: true, NoSigs: true})
	if err != nil {
		fmt.Println("nats:", err)
		os.Exit(1)
	}
	nats.Start()
	if !nats.ReadyForConnections(10 * time.Second) {
		fmt.Println("nats did not start")
		os.Exit(1)
	}

	users = newFakeUserService()

	cfg = config.LoadConfigOrPanic()
	cfg.NatsConfig.URL = nats.ClientURL()
	cfg.NatsConfig.ConsumerGroupName = "e2e"
	cfg.UserService.URL = users.server.URL

	dbService := db.NewDatabase(cfg.DBConfig)
	database = dbService.DB
	if err := database.Exec("SELECT 1 FROM feed_items LIMIT 1").Error; err != nil {
		fmt.Println("tables missing; run the migrations first:", err)
		os.Exit(1)
	}

	// The API and the consumers share one service graph, wired exactly as
	// the binary does it, including the real delivery publisher on NATS.
	ctx, cancel := context.WithCancel(context.Background())
	services := wiring.BuildWith(ctx, cfg, dbService, userServiceClient(users.server.URL), true)
	api = httptest.NewServer(handlers.BuildRootHandlerWithServices(cfg, services))

	handlersForConsumers := consumers.Handlers{Feed: services.Feed, Notifications: services.Notifications}
	go func() { _ = consumers.RunActivity(ctx, cfg, handlersForConsumers) }()
	go func() { _ = consumers.RunFollow(ctx, cfg, handlersForConsumers) }()

	producer = epnats.NewNatsDriver(&epnats.Config{URL: nats.ClientURL()}).(*epnats.NatsDriver)
	// Give the consumers a moment to create their durable consumers before
	// the first publish; JetStream keeps the message either way.
	time.Sleep(500 * time.Millisecond)

	code := m.Run()

	cancel()
	api.Close()
	users.server.Close()
	_ = producer.Close()
	services.Close()
	nats.Shutdown()
	os.Exit(code)
}
