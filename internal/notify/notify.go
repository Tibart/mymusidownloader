package notify

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Notifier posts messages to an ntfy topic.
type Notifier struct {
	BaseURL  string
	Topic    string
	ClickURL string
	HTTP     *http.Client
}

// New returns a Notifier. clickURL is opened when the user taps the message; empty means none.
func New(baseURL, topic, clickURL string) Notifier {
	return Notifier{BaseURL: strings.TrimRight(baseURL, "/"), Topic: topic, ClickURL: clickURL, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Send posts body with the given title. A failure is logged and returned.
func (n Notifier) Send(ctx context.Context, title, body string) error {
	endpoint := n.BaseURL + "/" + url.PathEscape(n.Topic)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("build notification: %w", err)
	}
	req.Header.Set("Title", title)
	if n.ClickURL != "" {
		req.Header.Set("Click", n.ClickURL)
	}
	resp, err := n.HTTP.Do(req)
	if err != nil {
		log.Printf("notify %q: %v", title, err)
		return fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		log.Printf("notify %q: status %d", title, resp.StatusCode)
		return fmt.Errorf("notification status %d", resp.StatusCode)
	}
	return nil
}
