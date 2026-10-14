package genre

import (
	"context"
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

func testClient(t *testing.T, body string, status int) (*Client, *int) {
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
		if status != http.StatusOK {
			http.Error(w, "boom", status)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	client := NewClient("0.0.0-test")
	client.BaseURL = srv.URL
	client.HTTP = srv.Client()
	client.MinInterval = 0
	return client, &calls
}

func TestFindPicksHighestCountedGenreTag(t *testing.T) {
	client, _ := testClient(t, metallicaResponse, http.StatusOK)
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
			client, _ := testClient(t, tt.body, http.StatusOK)
			got, err := client.Find(context.Background(), "Metallica")
			if err != nil || got != "" {
				t.Fatalf("Find = %q, %v; want no genre and no error", got, err)
			}
		})
	}
}

func TestFindServiceUnavailableIsError(t *testing.T) {
	client, _ := testClient(t, "", http.StatusServiceUnavailable)
	if _, err := client.Find(context.Background(), "Metallica"); err == nil {
		t.Fatal("Find on 503 returned no error")
	}
}

func TestFindHonoursTheRequestInterval(t *testing.T) {
	client, calls := testClient(t, metallicaResponse, http.StatusOK)
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
	client, _ := testClient(t, metallicaResponse, http.StatusOK)
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
	client, calls := testClient(t, metallicaResponse, http.StatusOK)
	if got, err := client.Find(context.Background(), "  "); got != "" || err != nil {
		t.Fatalf("Find = %q, %v", got, err)
	}
	if *calls != 0 {
		t.Fatalf("server calls = %d, want 0", *calls)
	}
}
