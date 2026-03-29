package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
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
	return &Client{
		baseURL:     baseURL,
		developerID: developerID,
		handler:     handler,
		httpClient:  &http.Client{Timeout: 0}, // no timeout for streaming
	}
}

// Run connects to the SSE stream and reads events until ctx is cancelled.
// Reconnects automatically with exponential backoff.
// Returns non-nil error only when context is done or permanent failure occurs.
func (c *Client) Run(ctx context.Context) error {
	var lastEventID string
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := c.connect(ctx, &lastEventID); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			delay := backoff(attempt)
			log.Printf("SSE connection error (attempt %d): %v — retrying in %v", attempt+1, err, delay)
			attempt++

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		attempt = 0
	}
}

func (c *Client) connect(ctx context.Context, lastEventID *string) error {
	url := fmt.Sprintf(
		"%s/api/v1/events/stream?developer_id=%s&event_types=REGISTRATION_OPENED,REGISTRATION_CLOSED",
		c.baseURL, c.developerID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	log.Printf("SSE connected to %s", url)

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	var (
		eventType string
		eventID   string
		dataLines []string
	)

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case line == "":
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

		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))

		case strings.HasPrefix(line, "id:"):
			eventID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))

		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))

		case strings.HasPrefix(line, ":"):
			// keepalive comment — ignore
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
