// Package genre finds a music genre for an artist through the MusicBrainz tag data.
package genre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
	// maxAttempts counts the first try plus the retries on a throttled answer.
	maxAttempts = 3
	// retryDelay is the wait before the second attempt. It doubles for the third.
	retryDelay = time.Second
	// maxRetryAfter ignores a Retry-After header that asks us to wait longer than a request may take.
	maxRetryAfter = 10 * time.Second
)

// ErrUnavailable reports that MusicBrainz kept throttling us, so the lookup is upstream trouble, not a bug.
var ErrUnavailable = errors.New("musicbrainz unavailable")

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
	// retryDelay is the first backoff after a throttled answer. Tests set it to 0.
	retryDelay time.Duration

	mu   sync.Mutex
	last time.Time
}

func NewClient(version string) *Client {
	return &Client{
		BaseURL:     defaultBaseURL,
		UserAgent:   fmt.Sprintf("mymusidownloader/%s ( https://github.com/Tibart/mymusidownloader )", version),
		HTTP:        &http.Client{Timeout: 10 * time.Second},
		MinInterval: minInterval,
		retryDelay:  retryDelay,
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
	var body searchResponse
	if err := c.search(ctx, c.BaseURL+"/artist/?"+query.Encode(), &body); err != nil {
		return "", err
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

// search decodes one search response, retrying while MusicBrainz answers 503 or 429.
func (c *Client) search(ctx context.Context, requestURL string, out *searchResponse) error {
	backoff := c.retryDelay
	for attempt := 1; ; attempt++ {
		status, retryAfter, err := c.attempt(ctx, requestURL, out)
		if err != nil {
			return err
		}
		if status == http.StatusOK {
			return nil
		}
		if !throttled(status) {
			return fmt.Errorf("musicbrainz status %d", status)
		}
		if attempt == maxAttempts {
			return fmt.Errorf("%w: status %d after %d attempts", ErrUnavailable, status, attempt)
		}
		delay := backoff
		if asked, ok := retryAfterDelay(retryAfter); ok {
			delay = asked
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		backoff *= 2
	}
}

// attempt makes one rate-limited request. It returns the status and the Retry-After header when the status is not 200.
func (c *Client) attempt(ctx context.Context, requestURL string, out *searchResponse) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, "", fmt.Errorf("build musicbrainz request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if err := c.wait(ctx); err != nil {
		return 0, "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("musicbrainz request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, resp.Header.Get("Retry-After"), nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return 0, "", fmt.Errorf("parse musicbrainz response: %w", err)
	}
	return http.StatusOK, "", nil
}

// throttled reports the statuses MusicBrainz uses when it is busy or we asked too often.
func throttled(status int) bool {
	return status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests
}

// retryAfterDelay reads a Retry-After header given in seconds, ignoring anything from maxRetryAfter upwards.
func retryAfterDelay(header string) (time.Duration, bool) {
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds < 0 {
		return 0, false
	}
	delay := time.Duration(seconds) * time.Second
	if delay >= maxRetryAfter {
		return 0, false
	}
	return delay, true
}

// wait holds the caller until at least MinInterval passed since the previous request.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := sleep(ctx, time.Until(c.last.Add(c.MinInterval))); err != nil {
		return err
	}
	c.last = time.Now()
	return nil
}

// sleep waits for delay and reports a cancelled context instead, also when no wait is left.
func sleep(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
