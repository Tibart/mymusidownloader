package youtube

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantURL string
		wantErr bool
	}{
		{name: "plain id", input: "dQw4w9WgXcQ", wantID: "dQw4w9WgXcQ", wantURL: WatchURL("dQw4w9WgXcQ")},
		{name: "watch url", input: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", wantID: "dQw4w9WgXcQ", wantURL: WatchURL("dQw4w9WgXcQ")},
		{name: "watch url with extra query", input: "https://youtube.com/watch?v=dQw4w9WgXcQ&t=10", wantID: "dQw4w9WgXcQ", wantURL: WatchURL("dQw4w9WgXcQ")},
		{name: "playlist rejected", input: "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL123", wantErr: true},
		{name: "channel rejected", input: "https://www.youtube.com/channel/UC1234567890", wantErr: true},
		{name: "search rejected", input: "https://www.youtube.com/results?search_query=test", wantErr: true},
		{name: "short url rejected", input: "https://youtu.be/dQw4w9WgXcQ", wantErr: true},
		{name: "bad id rejected", input: "not-an-id", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) error = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.input, err)
			}
			if got.VideoID != tt.wantID {
				t.Fatalf("VideoID = %q, want %q", got.VideoID, tt.wantID)
			}
			if got.URL != tt.wantURL {
				t.Fatalf("URL = %q, want %q", got.URL, tt.wantURL)
			}
		})
	}
}
