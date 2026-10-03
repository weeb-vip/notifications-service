# notifications-service

Feeds and inboxes for weeb.vip: a federated GraphQL subgraph plus two NATS
JetStream consumers. See `CLAUDE.md` for the data model, commands and tests.

```
list-service  --user-activity-->  consume activity --> activities + feed_items
user-service  --user-follow---->  consume follow   --> notifications (+ feed seed/clear)
                                                   --> notification-deliver (push / email workers)
GraphQL: feed, userActivity, notifications, unreadNotificationCount,
         notificationPreferences, pushSubscriptions and their mutations
```
