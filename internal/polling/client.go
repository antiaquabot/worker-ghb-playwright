package polling

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

// EventHandler matches the sse.EventHandler signature.
type EventHandler func(eventType, externalID string, data map[string]any)

type registrationStatus struct {
	DeveloperID      string `json:"developer_id"`
	ExternalID       string `json:"external_id"`
	Title            string `json:"title"`
	RegistrationOpen bool   `json:"registration_open"`
	RegistrationURL  string `json:"registration_url"`
}

// Client polls the REST endpoint as fallback when SSE is unavailable.
type Client struct {
	baseURL      string
	developerID  string
	intervalSecs int
	handler      EventHandler
	httpClient   *http.Client
	// previous state: externalID → registration_open
	prevState map[string]bool
}

func New(baseURL, developerID string, intervalSecs int, handler EventHandler) *Client {
	if intervalSecs <= 0 {
		intervalSecs = 60
	}
	return &Client{
		baseURL:      baseURL,
		developerID:  developerID,
		intervalSecs: intervalSecs,
		handler:      handler,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		prevState:    make(map[string]bool),
	}
}

// Run polls the REST endpoint at the configured interval until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	log.Printf("polling fallback started (interval=%ds, developer=%s)", c.intervalSecs, c.developerID)
	ticker := time.NewTicker(time.Duration(c.intervalSecs) * time.Second)
	defer ticker.Stop()

	// Initial poll
	c.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

func (c *Client) poll(ctx context.Context) {
	statuses, err := c.fetchStatuses(ctx)
	if err != nil {
		log.Printf("polling error: %v", err)
		return
	}

	current := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		current[s.ExternalID] = s.RegistrationOpen
	}

	// Detect changes vs previous state
	for id, open := range current {
		prev, seen := c.prevState[id]
		if !seen {
			// First time seeing this object — no event yet
			continue
		}
		if open && !prev {
			data := map[string]any{"external_id": id, "developer_id": c.developerID}
			for _, s := range statuses {
				if s.ExternalID == id {
					data["title"] = s.Title
					data["registration_url"] = s.RegistrationURL
					break
				}
			}
			c.handler("REGISTRATION_OPENED", id, data)
		} else if !open && prev {
			c.handler("REGISTRATION_CLOSED", id, map[string]any{
				"external_id": id, "developer_id": c.developerID,
			})
		}
	}

	c.prevState = current
}

func (c *Client) fetchStatuses(ctx context.Context) ([]registrationStatus, error) {
	u, err := url.Parse(c.baseURL + "/api/v1/registration-status")
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	q := u.Query()
	q.Set("developer_id", c.developerID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var statuses []registrationStatus
	if err := json.Unmarshal(body, &statuses); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return statuses, nil
}
