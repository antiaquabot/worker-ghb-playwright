package scenario

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stroi-homes/worker-ghb-playwright/internal/browser"
	"github.com/stroi-homes/worker-ghb-playwright/internal/config"
)

// Scenario performs browser-based auto-registration on a developer's website.
type Scenario interface {
	Execute(ctx context.Context, objectID string, personalData config.PersonalData) error
}

// GHBScenario implements Scenario for GHB via Playwright/Chromium.
// TODO: Implement actual registration scenario by analyzing GHB website UX flow.
type GHBScenario struct {
	manager *browser.Manager
}

func NewGHBScenario(manager *browser.Manager) *GHBScenario {
	return &GHBScenario{manager: manager}
}

// Execute runs the GHB browser registration scenario for a given object.
func (s *GHBScenario) Execute(ctx context.Context, objectID string, personalData config.PersonalData) error {
	pw, br, err := s.manager.Launch()
	if err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	defer func() {
		_ = br.Close()
		_ = pw.Stop()
	}()

	page, err := br.NewPage()
	if err != nil {
		return fmt.Errorf("new page: %w", err)
	}

	// Take screenshot on any error for debugging
	defer func() {
		if err != nil {
			s.saveScreenshot(page, objectID)
		}
	}()

	// TODO: Implement GHB registration scenario:
	// 1. Navigate to registration URL for objectID
	// 2. Fill in personalData fields (FullName, Phone, BirthDate, etc.)
	// 3. Submit the form
	// 4. Confirm success (check for confirmation element)
	//
	// Example skeleton (not functional):
	//   if _, err = page.Goto("https://ghb.ru/registration/" + objectID); err != nil {
	//       return fmt.Errorf("navigate: %w", err)
	//   }
	//   if err = page.Fill("#name-field", personalData.FullName); err != nil {
	//       return fmt.Errorf("fill name: %w", err)
	//   }
	//   ...

	log.Printf("GHBScenario.Execute: not yet implemented for object %s", objectID)
	return fmt.Errorf("scenario not implemented — see TODO in scenario/ghb.go")
}

// saveScreenshot saves a debug screenshot to the manager's screenshots dir.
func (s *GHBScenario) saveScreenshot(page playwright.Page, objectID string) {
	dir := s.manager.ScreenshotsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("failed to create screenshots dir: %v", err)
		return
	}
	ts := time.Now().Format("20060102_150405")
	path := filepath.Join(dir, fmt.Sprintf("%s_%s.png", ts, objectID))
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(path)}); err != nil {
		log.Printf("failed to save screenshot: %v", err)
		return
	}
	log.Printf("screenshot saved: %s", path)
}
