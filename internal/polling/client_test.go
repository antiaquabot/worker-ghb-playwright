package polling

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_FireOnFirstPoll_OpenRegistration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := []registrationStatus{
			{
				DeveloperID:      "ghb",
				ExternalID:       "obj-1",
				RegistrationOpen: true,
				RegistrationURL:  "https://example.com/register/obj-1",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statuses)
	}))
	defer srv.Close()

	var fired []string
	handler := func(eventType, externalID string, data map[string]any) {
		if eventType == "REGISTRATION_OPENED" {
			fired = append(fired, externalID)
		}
	}

	c := New(srv.URL, "ghb", 60, handler)
	c.fireOnFirst = true

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.poll(ctx)

	if len(fired) != 1 || fired[0] != "obj-1" {
		t.Errorf("expected REGISTRATION_OPENED for obj-1, got %v", fired)
	}
}

func TestClient_DefaultBehavior_NoFireOnFirstPoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := []registrationStatus{
			{ExternalID: "obj-1", RegistrationOpen: true},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statuses)
	}))
	defer srv.Close()

	var fired []string
	handler := func(eventType, externalID string, data map[string]any) {
		fired = append(fired, externalID)
	}

	c := New(srv.URL, "ghb", 60, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.poll(ctx)

	if len(fired) != 0 {
		t.Errorf("expected no events on first poll by default, got %v", fired)
	}
}
