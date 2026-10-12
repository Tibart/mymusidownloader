package setlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestVenueFromTitle(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{title: "Metallica - Live at Pinkpop 2019", want: "Pinkpop"},
		{title: "Band @ Ziggo Dome (Full Show)", want: "Ziggo Dome"},
		{title: "Band Live in Berlin [Official]", want: ""},
		{title: "Band - Song", want: ""},
		{title: "Live At The Roundhouse | Full", want: "The Roundhouse"},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := VenueFromTitle(tt.title); got != tt.want {
				t.Fatalf("VenueFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestFindMatchesExactArtistAndVenue(t *testing.T) {
	body := `{"setlist":[
	 {"eventDate":"01-01-2019","url":"https://x/tribute","artist":{"name":"Some Kind of Metallica"},
	  "venue":{"name":"Pinkpop Club","city":{"name":"Ghent","country":{"code":"BE"}}}},
	 {"eventDate":"09-06-2019","url":"https://x/real","artist":{"name":"Metallica"},
	  "venue":{"name":"Pinkpop Festival","city":{"name":"Landgraaf","country":{"code":"NL"}}}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
			return
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()
	client := Client{APIKey: "key", BaseURL: srv.URL, HTTP: srv.Client()}

	event, ok, err := client.Find(context.Background(), "Metallica", "2019", "Pinkpop")
	if err != nil || !ok {
		t.Fatalf("Find = %v, %v; want match", ok, err)
	}
	if event.Venue != "Pinkpop Festival" || event.Date != "2019-06-09" || event.URL != "https://x/real" {
		t.Fatalf("event = %+v", event)
	}
}

func TestFindNotFoundIsNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":404}`, http.StatusNotFound)
	}))
	defer srv.Close()
	client := Client{APIKey: "key", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, ok, err := client.Find(context.Background(), "Metallica", "2019", "Pinkpop"); ok || err != nil {
		t.Fatalf("Find on 404 = %v, %v; want no match, no error", ok, err)
	}
}

func TestFindBadKeyIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()
	client := Client{APIKey: "bad", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, _, err := client.Find(context.Background(), "Metallica", "2019", "Pinkpop"); err == nil {
		t.Fatal("Find with 403 returned no error")
	}
}

func TestLiveFind(t *testing.T) {
	key := os.Getenv("SETLIST_LIVE_KEY")
	if key == "" {
		t.Skip("SETLIST_LIVE_KEY not set")
	}
	event, ok, err := NewClient(key).Find(context.Background(), "Metallica", "2019", "Chase Center")
	if err != nil || !ok {
		t.Fatalf("live Find = %v, %v; want match", ok, err)
	}
	if event.Date != "2019-09-08" {
		t.Fatalf("live date = %q", event.Date)
	}
}
