package setlist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.setlist.fm/rest/1.0"

var venueHintPattern = regexp.MustCompile(`(?i)(?:\blive\s+(?:at|from)\s+|@\s*)([^(\[|]+?)(?:\s*[(\[|]|\s+\d{4}\b|\s*$)`)

type Event struct {
	Venue   string
	City    string
	Country string
	Date    string
	URL     string
}

func (e Event) String() string {
	return fmt.Sprintf("%s, %s, %s, %s; %s", e.Venue, e.City, e.Country, e.Date, e.URL)
}

type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

func NewClient(apiKey string) Client {
	return Client{APIKey: apiKey, BaseURL: defaultBaseURL, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// VenueFromTitle returns the venue after "Live at", "Live from" or "@", or "". "Live in" is usually a city and is ignored.
func VenueFromTitle(title string) string {
	m := venueHintPattern.FindStringSubmatch(title)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

type searchResponse struct {
	Setlist []struct {
		EventDate string `json:"eventDate"`
		URL       string `json:"url"`
		Artist    struct {
			Name string `json:"name"`
		} `json:"artist"`
		Venue struct {
			Name string `json:"name"`
			City struct {
				Name    string `json:"name"`
				Country struct {
					Code string `json:"code"`
				} `json:"country"`
			} `json:"city"`
		} `json:"venue"`
	} `json:"setlist"`
}

// Find returns the first setlist whose artist matches exactly and whose venue contains venueHint.
// A 404 from setlist.fm means no match and is not an error.
func (c Client) Find(ctx context.Context, artist, year, venueHint string) (Event, bool, error) {
	query := url.Values{"artistName": {artist}, "year": {year}, "venueName": {venueHint}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/search/setlists?"+query.Encode(), nil)
	if err != nil {
		return Event{}, false, fmt.Errorf("build setlist request: %w", err)
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mymusidownloader")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Event{}, false, fmt.Errorf("setlist.fm request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Event{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Event{}, false, fmt.Errorf("setlist.fm status %d", resp.StatusCode)
	}
	var body searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Event{}, false, fmt.Errorf("parse setlist.fm response: %w", err)
	}
	for _, s := range body.Setlist {
		if !strings.EqualFold(strings.TrimSpace(s.Artist.Name), strings.TrimSpace(artist)) {
			continue
		}
		if !strings.Contains(strings.ToLower(s.Venue.Name), strings.ToLower(venueHint)) {
			continue
		}
		date, err := time.Parse("02-01-2006", s.EventDate)
		if err != nil {
			return Event{}, false, fmt.Errorf("parse event date %q: %w", s.EventDate, err)
		}
		return Event{
			Venue:   s.Venue.Name,
			City:    s.Venue.City.Name,
			Country: s.Venue.City.Country.Code,
			Date:    date.Format("2006-01-02"),
			URL:     s.URL,
		}, true, nil
	}
	return Event{}, false, nil
}

// Lookup returns a track.EventLookup-compatible func. It returns "" when the title has no venue or nothing matches, and an error on API or network failure.
func Lookup(client Client) func(ctx context.Context, artist, year, title string) (string, error) {
	return func(ctx context.Context, artist, year, title string) (string, error) {
		venue := VenueFromTitle(title)
		if venue == "" {
			return "", nil
		}
		event, ok, err := client.Find(ctx, artist, year, venue)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", nil
		}
		return event.String(), nil
	}
}
