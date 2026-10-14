// Package genre finds a music genre for an artist through the MusicBrainz tag data.
package genre

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://musicbrainz.org/ws/2"
	// minScore keeps only a near-certain artist match from the search index.
	minScore = 90
	// minTagCount drops tags that only one person applied.
	minTagCount = 2
	// minInterval is the one request per second MusicBrainz asks anonymous clients to respect.
	minInterval = time.Second
)

// skipTags are common MusicBrainz tags that describe something other than a genre.
var skipTags = map[string]bool{
	"seen live":        true,
	"female vocalists": true,
	"male vocalists":   true,
	"american":         true,
	"british":          true,
	"canadian":         true,
	"dutch":            true,
	"french":           true,
	"german":           true,
	"japanese":         true,
	"swedish":          true,
}

type Client struct {
	BaseURL     string
	UserAgent   string
	HTTP        *http.Client
	MinInterval time.Duration

	mu   sync.Mutex
	last time.Time
}

func NewClient(version string) *Client {
	return &Client{
		BaseURL:     defaultBaseURL,
		UserAgent:   fmt.Sprintf("mymusidownloader/%s ( https://github.com/Tibart/mymusidownloader )", version),
		HTTP:        &http.Client{Timeout: 10 * time.Second},
		MinInterval: minInterval,
	}
}

type searchResponse struct {
	Artists []struct {
		Name  string `json:"name"`
		Score int    `json:"score"`
		Tags  []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"tags"`
	} `json:"artists"`
}

// Find returns the best MusicBrainz tag for artist, title cased, or "" when nothing qualifies.
func (c *Client) Find(ctx context.Context, artist string) (string, error) {
	artist = strings.TrimSpace(artist)
	if artist == "" {
		return "", nil
	}
	query := url.Values{"query": {`artist:"` + artist + `"`}, "fmt": {"json"}, "limit": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/artist/?"+query.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("build musicbrainz request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if err := c.wait(ctx); err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("musicbrainz request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("musicbrainz status %d", resp.StatusCode)
	}
	var body searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("parse musicbrainz response: %w", err)
	}
	if len(body.Artists) == 0 {
		return "", nil
	}
	first := body.Artists[0]
	if first.Score < minScore || !strings.EqualFold(strings.TrimSpace(first.Name), artist) {
		return "", nil
	}
	best, bestCount := "", 0
	for _, tag := range first.Tags {
		name := strings.TrimSpace(tag.Name)
		if name == "" || tag.Count < minTagCount || skipTags[strings.ToLower(name)] {
			continue
		}
		if tag.Count > bestCount {
			best, bestCount = name, tag.Count
		}
	}
	return titleCase(best), nil
}

// wait holds the caller until at least MinInterval passed since the previous request.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if delay := time.Until(c.last.Add(c.MinInterval)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	c.last = time.Now()
	return nil
}

// Lookup returns a track.GenreLookup-compatible func. It keeps the spelling of a known genre when one matches.
func Lookup(client *Client, known func() []string) func(ctx context.Context, artist string) (string, error) {
	return func(ctx context.Context, artist string) (string, error) {
		found, err := client.Find(ctx, artist)
		if err != nil || found == "" {
			return "", err
		}
		if known != nil {
			for _, candidate := range known() {
				if strings.EqualFold(candidate, found) {
					return candidate, nil
				}
			}
		}
		return found, nil
	}
}

func titleCase(value string) string {
	fields := strings.Fields(value)
	for i, word := range fields {
		runes := []rune(word)
		fields[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(fields, " ")
}
