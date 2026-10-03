// Package userservice asks user-service about users, straight at the
// subgraph. No gateway, no token: the consumers act for no one, and the two
// queries they use are public.
package userservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is what the feed needs to know about users.
type Client interface {
	// ListsPublic reports whether the user shows their lists. Unknown users
	// are reported as not public, so their activity is never fanned out.
	ListsPublic(ctx context.Context, userID string) (bool, error)
	// FollowerIDs pages accepted follower ids after the given id, in id order.
	FollowerIDs(ctx context.Context, userID, after string, limit int) ([]string, error)
}

type client struct {
	url  string
	http *http.Client
}

// New returns a client for the subgraph at url.
func New(url string) Client {
	return &client{url: url, http: &http.Client{Timeout: 10 * time.Second}}
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *client) do(ctx context.Context, query string, variables map[string]any, into any) error {
	body, err := json.Marshal(graphqlRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("userservice: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("userservice: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("userservice: call: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("userservice: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("userservice: status %d: %s", resp.StatusCode, raw)
	}

	var out graphqlResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("userservice: decode: %w", err)
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("userservice: %s", out.Errors[0].Message)
	}

	return json.Unmarshal(out.Data, into)
}

func (c *client) ListsPublic(ctx context.Context, userID string) (bool, error) {
	var out struct {
		PublicUserByID *struct {
			ListsPublic bool `json:"listsPublic"`
		} `json:"publicUserByID"`
	}
	err := c.do(ctx, `query($id: ID!) { publicUserByID(id: $id) { listsPublic } }`, map[string]any{"id": userID}, &out)
	if err != nil {
		return false, err
	}
	if out.PublicUserByID == nil {
		return false, nil
	}

	return out.PublicUserByID.ListsPublic, nil
}

func (c *client) FollowerIDs(ctx context.Context, userID, after string, limit int) ([]string, error) {
	var out struct {
		FollowerIDs []string `json:"followerIDs"`
	}
	variables := map[string]any{"id": userID, "limit": limit}
	if after != "" {
		variables["after"] = after
	}
	err := c.do(ctx, `query($id: ID!, $after: ID, $limit: Int!) { followerIDs(userID: $id, after: $after, limit: $limit) }`, variables, &out)
	if err != nil {
		return nil, err
	}

	return out.FollowerIDs, nil
}
