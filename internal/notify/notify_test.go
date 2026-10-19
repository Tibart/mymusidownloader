package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendPostsBodyWithTitleToTopic(t *testing.T) {
	var gotPath, gotTitle, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTitle = r.Header.Get("Title")
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
	}))
	defer srv.Close()

	if err := New(srv.URL+"/", "musi-abc", "").Send(context.Background(), "Downloaded", "Band: Song"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/musi-abc" || gotTitle != "Downloaded" || gotBody != "Band: Song" {
		t.Fatalf("path=%q title=%q body=%q", gotPath, gotTitle, gotBody)
	}
}

func TestSendReturnsErrorOnServerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if err := New(srv.URL, "t", "").Send(context.Background(), "x", "y"); err == nil {
		t.Fatal("expected an error on 503")
	}
}

func TestSendSetsClickHeaderToAppURL(t *testing.T) {
	var click string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		click = r.Header.Get("Click")
	}))
	defer srv.Close()
	if err := New(srv.URL, "t", "http://app.example:6874/").Send(context.Background(), "x", "y"); err != nil {
		t.Fatal(err)
	}
	if click != "http://app.example:6874/" {
		t.Fatalf("Click = %q", click)
	}
}
