package monitor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// SlackChannelInfo is the compact channel shape used by the channel picker.
type SlackChannelInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsPrivate bool   `json:"is_private"`
	IsMember  bool   `json:"is_member"`
}

type slackChannelCache struct {
	CachedAt string             `json:"cached_at"`
	Channels []SlackChannelInfo `json:"channels"`
}

// slackConversationsFn is the mockable seam that hits conversations.list.
var slackConversationsFn = func(ctx context.Context, token string) ([]SlackChannelInfo, error) {
	api := slack.New(token)
	var out []SlackChannelInfo
	cursor := ""
	for {
		channels, next, err := api.GetConversationsContext(ctx, &slack.GetConversationsParameters{
			Types:           []string{"public_channel", "private_channel"},
			ExcludeArchived: true,
			Limit:           200,
			Cursor:          cursor,
		})
		if err != nil {
			return nil, err
		}
		for _, c := range channels {
			out = append(out, SlackChannelInfo{
				ID:        c.ID,
				Name:      c.Name,
				IsPrivate: c.IsPrivate,
				IsMember:  c.IsMember,
			})
		}
		if strings.TrimSpace(next) == "" {
			break
		}
		cursor = next
	}
	return out, nil
}

// ListSlackChannels returns the channels visible to the configured bot token.
// When no token is configured it returns an empty list (not an error) so the
// UI can render a "configure Slack" empty state gracefully.
func ListSlackChannels(ctx context.Context) ([]SlackChannelInfo, error) {
	token := SlackBotToken()
	if strings.TrimSpace(token) == "" {
		if cached, ok := readSlackChannelCache(); ok {
			return cached, nil
		}
		return nil, nil
	}
	channels, err := slackConversationsFn(ctx, token)
	if err != nil {
		if cached, ok := readSlackChannelCache(); ok {
			return cached, nil
		}
		return nil, err
	}
	writeSlackChannelCache(channels)
	return channels, nil
}

func slackChannelCachePath() string {
	root := strings.TrimSpace(os.Getenv("FLOW_ROOT"))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return ""
		}
		root = filepath.Join(home, ".flow")
	}
	return filepath.Join(root, "cache", "slack_channels.json")
}

func readSlackChannelCache() ([]SlackChannelInfo, bool) {
	path := slackChannelCachePath()
	if path == "" {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var cache slackChannelCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		return nil, false
	}
	channels := compactSlackChannels(cache.Channels)
	if len(channels) == 0 {
		return nil, false
	}
	return channels, true
}

func writeSlackChannelCache(channels []SlackChannelInfo) {
	path := slackChannelCachePath()
	if path == "" {
		return
	}
	channels = compactSlackChannels(channels)
	if len(channels) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	cache := slackChannelCache{
		CachedAt: time.Now().UTC().Format(time.RFC3339),
		Channels: channels,
	}
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func compactSlackChannels(channels []SlackChannelInfo) []SlackChannelInfo {
	var out []SlackChannelInfo
	seen := map[string]bool{}
	for _, ch := range channels {
		id := strings.TrimSpace(ch.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(ch.Name)
		out = append(out, SlackChannelInfo{
			ID:        id,
			Name:      name,
			IsPrivate: ch.IsPrivate,
			IsMember:  ch.IsMember,
		})
	}
	return out
}
