package genre

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// metallicaResponse is a trimmed recording of GET /ws/2/artist/?query=artist:"Metallica"&fmt=json&limit=1.
const metallicaResponse = `{"created":"2024-07-06T10:00:00.000Z","count":1,"offset":0,"artists":[
 {"id":"65f4f0c5-ef9e-490c-aee3-909e7ae6b2ab","type":"Group","score":100,"name":"Metallica","sort-name":"Metallica","country":"US",
  "tags":[{"count":14,"name":"american"},{"count":11,"name":"seen live"},{"count":9,"name":"thrash metal"},{"count":4,"name":"heavy metal"},{"count":1,"name":"speed metal"}]}]}`

func testClient(t *testing.T, body string) (*Client, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "mymusidownloader/") {
			t.Errorf("User-Agent = %q", ua)
		}
		if accept := r.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("Accept = %q", accept)
		}
		if query := r.URL.Query(); query.Get("query") != `artist:"Metallica"` || query.Get("fmt") != "json" || query.Get("limit") != "1" {
			t.Errorf("query = %v", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	client := NewClient("0.0.0-test")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()
	client.MinInterval = 0
	client.retryDelay = 0
	return client, &calls
}

// reply is one scripted MusicBrainz answer.
type reply struct {
	status     int
	body       string
	retryAfter string
}

// scriptedClient answers each request with the next reply and repeats the last one after that.
func scriptedClient(t *testing.T, replies ...reply) (*Client, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next := replies[len(replies)-1]
		if calls < len(replies) {
			next = replies[calls]
		}
		calls++
		if next.retryAfter != "" {
			w.Header().Set("Retry-After", next.retryAfter)
		}
		if next.status != http.StatusOK {
			http.Error(w, "boom", next.status)
			return
		}
		_, _ = w.Write([]byte(next.body))
	}))
	t.Cleanup(srv.Close)
	client := NewClient("0.0.0-test")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()
	client.MinInterval = 0
	client.retryDelay = 0
	return client, &calls
}

func TestFindPicksHighestCountedGenreTag(t *testing.T) {
	client, _ := testClient(t, metallicaResponse)
	got, err := client.Find(context.Background(), "Metallica")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Thrash Metal" {
		t.Fatalf("Find = %q, want Thrash Metal", got)
	}
}

func TestFindRejectsWeakMatches(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "no artists", body: `{"artists":[]}`},
		{name: "low score", body: `{"artists":[{"name":"Metallica","score":70,"tags":[{"count":9,"name":"thrash metal"}]}]}`},
		{name: "wrong name", body: `{"artists":[{"name":"Metallica Tribute","score":98,"tags":[{"count":9,"name":"thrash metal"}]}]}`},
		{name: "only skipped or thin tags", body: `{"artists":[{"name":"Metallica","score":100,"tags":[{"count":14,"name":"american"},{"count":1,"name":"speed metal"}]}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := testClient(t, tt.body)
			got, err := client.Find(context.Background(), "Metallica")
			if err != nil || got != "" {
				t.Fatalf("Find = %q, %v; want no genre and no error", got, err)
			}
		})
	}
}

func TestFindRetriesAfterServiceUnavailable(t *testing.T) {
	client, calls := scriptedClient(t,
		reply{status: http.StatusServiceUnavailable},
		reply{status: http.StatusOK, body: metallicaResponse},
	)
	got, err := client.Find(context.Background(), "Metallica")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Thrash Metal" {
		t.Fatalf("Find = %q, want Thrash Metal", got)
	}
	if *calls != 2 {
		t.Fatalf("server calls = %d, want 2", *calls)
	}
}

func TestFindReturnsErrUnavailableAfterThreeAttempts(t *testing.T) {
	client, calls := scriptedClient(t, reply{status: http.StatusServiceUnavailable})
	_, err := client.Find(context.Background(), "Metallica")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Find on repeated 503 = %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error %q does not name the status", err)
	}
	if *calls != 3 {
		t.Fatalf("server calls = %d, want 3", *calls)
	}
}

func TestFindHonoursRetryAfterOnTooManyRequests(t *testing.T) {
	client, calls := scriptedClient(t,
		reply{status: http.StatusTooManyRequests, retryAfter: "0"},
		reply{status: http.StatusOK, body: metallicaResponse},
	)
	client.retryDelay = time.Second
	start := time.Now()
	got, err := client.Find(context.Background(), "Metallica")
	if err != nil || got != "Thrash Metal" {
		t.Fatalf("Find = %q, %v; want Thrash Metal", got, err)
	}
	if elapsed := time.Since(start); elapsed >= client.retryDelay {
		t.Fatalf("retry took %s, want the Retry-After of 0 seconds", elapsed)
	}
	if *calls != 2 {
		t.Fatalf("server calls = %d, want 2", *calls)
	}
}

func TestFindDoesNotRetryNotFound(t *testing.T) {
	client, calls := scriptedClient(t, reply{status: http.StatusNotFound})
	_, err := client.Find(context.Background(), "Metallica")
	if err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("Find on 404 = %v, want a plain status error", err)
	}
	if *calls != 1 {
		t.Fatalf("server calls = %d, want 1", *calls)
	}
}

func TestFindStopsRetryingOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "boom", http.StatusServiceUnavailable)
		cancel()
	}))
	t.Cleanup(srv.Close)
	client := NewClient("0.0.0-test")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()
	client.MinInterval = 0
	client.retryDelay = time.Second
	if _, err := client.Find(ctx, "Metallica"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Find with cancelled context = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("server calls = %d, want 1", calls)
	}
}

func TestFindHonoursTheRequestInterval(t *testing.T) {
	client, calls := testClient(t, metallicaResponse)
	client.MinInterval = 150 * time.Millisecond
	start := time.Now()
	for i := 0; i < 2; i++ {
		if _, err := client.Find(context.Background(), "Metallica"); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < client.MinInterval {
		t.Fatalf("two lookups took %s, want at least %s", elapsed, client.MinInterval)
	}
	if *calls != 2 {
		t.Fatalf("server calls = %d, want 2", *calls)
	}
}

func TestLookupKeepsKnownGenreSpelling(t *testing.T) {
	client, _ := testClient(t, metallicaResponse)
	lookup := Lookup(client, func() []string { return []string{"Rock", "thrash METAL"} })
	got, err := lookup(context.Background(), "Metallica")
	if err != nil {
		t.Fatal(err)
	}
	if got != "thrash METAL" {
		t.Fatalf("Lookup = %q, want the known spelling", got)
	}
}

func TestFindWithoutArtistDoesNotCallTheAPI(t *testing.T) {
	client, calls := testClient(t, metallicaResponse)
	if got, err := client.Find(context.Background(), "  "); got != "" || err != nil {
		t.Fatalf("Find = %q, %v", got, err)
	}
	if *calls != 0 {
		t.Fatalf("server calls = %d, want 0", *calls)
	}
}
