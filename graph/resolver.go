package graph

import (
	"github.com/weeb-vip/notifications-service/config"
	"github.com/weeb-vip/notifications-service/internal/services/feed"
	"github.com/weeb-vip/notifications-service/internal/services/notifications"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

type Resolver struct {
	Config               config.Config
	FeedService          feed.Service
	NotificationsService notifications.Service
}
