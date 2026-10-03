package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"

	"github.com/weeb-vip/notifications-service/config"
	"github.com/weeb-vip/notifications-service/graph"
	"github.com/weeb-vip/notifications-service/graph/generated"
	"github.com/weeb-vip/notifications-service/http/handlers/logger"
	"github.com/weeb-vip/notifications-service/http/handlers/requestinfo"
	"github.com/weeb-vip/notifications-service/internal/directives"
	"github.com/weeb-vip/notifications-service/internal/wiring"
)

// BuildRootHandler builds the GraphQL handler over freshly built services.
func BuildRootHandler(conf config.Config) http.Handler {
	return BuildRootHandlerWithContext(context.Background(), conf)
}

// BuildRootHandlerWithContext is BuildRootHandler with a context for the
// service graph's logger and tracer.
func BuildRootHandlerWithContext(ctx context.Context, conf config.Config) http.Handler {
	services := wiring.Build(ctx, conf, false)

	return BuildRootHandlerWithServices(conf, services)
}

// BuildRootHandlerWithServices wires an already-built service graph, which is
// how the end-to-end tests share one database connection with the handler.
func BuildRootHandlerWithServices(conf config.Config, services *wiring.Services) http.Handler {
	resolvers := &graph.Resolver{
		Config:               conf,
		FeedService:          services.Feed,
		NotificationsService: services.Notifications,
	}

	cfg := generated.Config{Resolvers: resolvers, Directives: directives.GetDirectives()}
	cfg.Directives.Authenticated = func(ctx context.Context, obj interface{}, next graphql.Resolver) (res interface{}, err error) {
		req := requestinfo.FromContext(ctx)
		if req.UserID == nil {
			return nil, fmt.Errorf("Access denied")
		}

		return next(ctx)
	}

	srv := handler.NewDefaultServer(generated.NewExecutableSchema(cfg))

	return requestinfo.Handler()(logger.Handler()(srv))
}
