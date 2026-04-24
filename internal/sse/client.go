package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EventHandler is called for each received SSE event.
type EventHandler func(eventType, externalID string, data map[string]any)

// Client connects to the SSE stream and calls handler for each event.
type Client struct {
	baseURL     string
	developerID string
	handler     EventHandler
	httpClient  *http.Client
}

func New(baseURL, developerID string, handler EventHandler) *Client {
	httpClient := &http.Client{
		Timeout: 0, // no timeout for streaming
	}
	return &Client{
		baseURL:     baseURL,
		developerID: developerID,
		handler:     handler,
		httpClient:  httpClient,
	}
}

// Run connects to the SSE stream and reads events until ctx is cancelled.
// Reconnects automatically with exponential backoff.
// Returns non-nil error only when context is done or permanent failure occurs.
// stableConnectionThreshold is the minimum uptime after which a dropped
// connection is considered "stable": the backoff counter resets so the next
// reconnect starts at 1 s instead of the accumulated maximum.
const stableConnectionThreshold = 30 * time.Second

func (c *Client) Run(ctx context.Context) error {
	var lastEventID string
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		start := time.Now()
		err := c.connect(ctx, &lastEventID)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// If the connection was alive long enough, treat it as stable and
		// reset the backoff so the next reconnect starts from 1 s.
		if time.Since(start) >= stableConnectionThreshold {
			attempt = 0
		}

		delay := backoff(attempt)
		log.Printf("SSE connection error (attempt %d): %v — retrying in %v", attempt+1, err, delay)
		attempt++

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (c *Client) connect(ctx context.Context, lastEventID *string) error {
	u, err := url.Parse(c.baseURL + "/api/v1/events/stream")
	if err != nil {
		return fmt.Errorf("parse base URL: %w", err)
	}
	q := u.Query()
	q.Set("developer_id", c.developerID)
	q.Set("event_types", "REGISTRATION_OPENED,REGISTRATION_CLOSED")
	u.RawQuery = q.Encode()
	reqURL := u.String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if *lastEventID != "" {
		req.Header.Set("Last-Event-ID", *lastEventID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	log.Printf("SSE connected to %s", reqURL)

	// Parse SSE stream.
	// Use a 1 MiB token buffer so large JSON payloads do not trigger
	// bufio.ErrTooLong (default is 64 KiB), which would silently drop the
	// current event and cause an immediate reconnect.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var (
		eventType string
		eventID   string
		dataLines []string
	)

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = dispatch event
			if len(dataLines) > 0 && eventType != "" {
				rawData := strings.Join(dataLines, "\n")
				var payload map[string]any
				if err := json.Unmarshal([]byte(rawData), &payload); err == nil {
					externalID, _ := payload["external_id"].(string)
					c.handler(eventType, externalID, payload)
				}
				if eventID != "" {
					*lastEventID = eventID
				}
			}
			// Reset fields
			eventType = ""
			eventID = ""
			dataLines = dataLines[:0]
		} else if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "id:") {
			eventID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return fmt.Errorf("stream ended")
}

// backoff returns exponential backoff delay capped at 60 seconds.
func backoff(attempt int) time.Duration {
	seconds := math.Pow(2, float64(attempt))
	if seconds > 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}
