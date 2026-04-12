package scenario

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stroi-homes/worker-ghb-playwright/internal/browser"
	"github.com/stroi-homes/worker-ghb-playwright/internal/config"
)

const (
	regBaseURL     = "https://reg.ghb.by"
	navTimeout     = 30 * time.Second
	smsWaitTimeout = 3 * time.Minute
)

// Scenario performs browser-based auto-registration on a developer's website.
type Scenario interface {
	// Execute runs the registration scenario for objectID.
	// smsCodeFn is called when the browser is on the SMS confirmation step —
	// it should block until the user provides the 6-digit code.
	Execute(ctx context.Context, objectID string, regURL string, personalData config.PersonalData, smsCodeFn SMSCodeFunc) error
}

// SMSCodeFunc blocks until the user provides an SMS confirmation code.
type SMSCodeFunc func(ctx context.Context) (string, error)

// GHBScenario implements Scenario for GHB via Playwright/Chromium.
//
// Registration flow mirrors the HTTP flow but uses a real browser:
//  1. Navigate to https://reg.ghb.by/register/?id=<objectID>
//  2. Fill in the registration form (last name, first name, patronymic, phone, consent)
//  3. Submit → server sends SMS
//  4. Wait for SMS code (via smsCodeFn callback)
//  5. Fill in SMS code and submit confirmation form
//  6. Verify success message
type GHBScenario struct {
	manager *browser.Manager
}

func NewGHBScenario(mgr *browser.Manager) *GHBScenario {
	return &GHBScenario{manager: mgr}
}

// Execute runs the GHB browser registration scenario.
func (s *GHBScenario) Execute(
	ctx context.Context,
	objectID string,
	regURL string,
	pd config.PersonalData,
	smsCodeFn SMSCodeFunc,
) error {
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

	// Screenshot on any error for debugging (FR-WORKER-PW-002)
	var execErr error
	defer func() {
		if execErr != nil {
			s.saveScreenshot(page, objectID)
		}
	}()

	execErr = s.runScenario(ctx, page, objectID, regURL, pd, smsCodeFn)
	return execErr
}

func (s *GHBScenario) runScenario(
	ctx context.Context,
	page playwright.Page,
	objectID string,
	regURL string,
	pd config.PersonalData,
	smsCodeFn SMSCodeFunc,
) error {
	toMs := float64(navTimeout.Milliseconds())

	// -----------------------------------------------------------------------
	// Step 1: Navigate to registration page
	// -----------------------------------------------------------------------
	log.Printf("[ghb-scenario] step 1: navigate to %s", regURL)
	if _, err := page.Goto(regURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(toMs),
	}); err != nil {
		return fmt.Errorf("step 1 navigate: %w", err)
	}

	// Check if already registered
	content, _ := page.Content()
	if isAlreadyRegistered(content) {
		return fmt.Errorf("already registered for object %s", objectID)
	}

	// -----------------------------------------------------------------------
	// Step 2: Fill registration form
	// -----------------------------------------------------------------------
	last, first, middle := pd.Parts()
	if last == "" || first == "" {
		return fmt.Errorf("personal_data: last_name and first_name are required")
	}
	phone := pd.PhoneDigits()
	if len(phone) != 9 {
		return fmt.Errorf("personal_data: phone must be 9 digits, got %q", phone)
	}

	log.Printf("[ghb-scenario] step 2: filling registration form")
	if err := page.Fill(`input[name="lastname"]`, last); err != nil {
		return fmt.Errorf("fill lastname: %w", err)
	}
	if err := page.Fill(`input[name="firstname"]`, first); err != nil {
		return fmt.Errorf("fill firstname: %w", err)
	}
	if err := page.Fill(`input[name="middlename"]`, middle); err != nil {
		// middlename may be optional — log and continue
		log.Printf("[ghb-scenario] warn: middlename field not found or fill failed: %v", err)
	}
	if err := page.Fill(`input[name="phone"]`, phone); err != nil {
		return fmt.Errorf("fill phone: %w", err)
	}

	// Tick consent checkbox if not already checked
	checked, _ := page.IsChecked(`input[name="consent"]`)
	if !checked {
		if err := page.Check(`input[name="consent"]`); err != nil {
			log.Printf("[ghb-scenario] warn: consent checkbox: %v", err)
		}
	}

	// -----------------------------------------------------------------------
	// Step 3: Submit form (act=reg_user)
	// -----------------------------------------------------------------------
	log.Printf("[ghb-scenario] step 3: submitting registration form")

	// Click the submit button; fall back to form submission
	submitErr := page.Click(`button[type="submit"], input[type="submit"]`)
	if submitErr != nil {
		// Try form.submit() via JS
		if _, jsErr := page.Evaluate(`
			var form = document.querySelector('form');
			if (form) {
				var act = document.querySelector('input[name="act"]');
				if (!act) {
					act = document.createElement('input');
					act.type = 'hidden';
					act.name = 'act';
					form.appendChild(act);
				}
				act.value = 'reg_user';
				form.submit();
			}
		`); jsErr != nil {
			return fmt.Errorf("submit form: button click failed (%v), JS fallback failed (%v)", submitErr, jsErr)
		}
	}

	// Wait for page to load (SMS form or success page)
	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State:   playwright.LoadStateNetworkidle,
		Timeout: playwright.Float(toMs),
	}); err != nil {
		log.Printf("[ghb-scenario] warn: wait after submit: %v", err)
	}

	// Check for immediate success (rare)
	content2, _ := page.Content()
	if isSuccess(content2) {
		log.Printf("[ghb-scenario] registration completed without SMS step")
		return nil
	}
	if isAlreadyRegistered(content2) {
		return fmt.Errorf("already registered for object %s", objectID)
	}
	if !hasSMSForm(content2) {
		return fmt.Errorf("step 3: SMS code form not found after form submission")
	}
	log.Printf("[ghb-scenario] step 3 OK — SMS form confirmed, waiting for code")

	// -----------------------------------------------------------------------
	// Step 4: Wait for SMS code
	// -----------------------------------------------------------------------
	smsCtx, smsCancel := context.WithTimeout(ctx, smsWaitTimeout)
	defer smsCancel()

	code, err := smsCodeFn(smsCtx)
	if err != nil {
		return fmt.Errorf("waiting for SMS code: %w", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("empty SMS code received")
	}
	log.Printf("[ghb-scenario] SMS code received, entering confirmation")

	// -----------------------------------------------------------------------
	// Step 5: Fill SMS code and submit
	// -----------------------------------------------------------------------
	if err := page.Fill(`input[name="sms_code"]`, code); err != nil {
		return fmt.Errorf("fill sms_code: %w", err)
	}

	// Click confirm button
	confSubmitErr := page.Click(`button[type="submit"], input[type="submit"]`)
	if confSubmitErr != nil {
		if _, jsErr := page.Evaluate(`
			var form = document.querySelector('form');
			if (form) {
				var act = document.querySelector('input[name="act"]');
				if (!act) {
					act = document.createElement('input');
					act.type = 'hidden';
					act.name = 'act';
					form.appendChild(act);
				}
				act.value = 'conf_user';
				form.submit();
			}
		`); jsErr != nil {
			return fmt.Errorf("submit confirmation: %v; JS fallback: %v", confSubmitErr, jsErr)
		}
	}

	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State:   playwright.LoadStateNetworkidle,
		Timeout: playwright.Float(toMs),
	}); err != nil {
		log.Printf("[ghb-scenario] warn: wait after confirmation: %v", err)
	}

	// -----------------------------------------------------------------------
	// Step 6: Verify success
	// -----------------------------------------------------------------------
	finalContent, _ := page.Content()
	if isSuccess(finalContent) {
		log.Printf("[ghb-scenario] registration completed successfully for object %s", objectID)
		return nil
	}
	if strings.Contains(strings.ToLower(finalContent), "неверн") {
		return fmt.Errorf("step 5: SMS code is incorrect")
	}

	return fmt.Errorf("step 5: registration outcome unclear — check %s manually", regURL)
}

// ---------------------------------------------------------------------------
// Screenshot helper
// ---------------------------------------------------------------------------

func (s *GHBScenario) saveScreenshot(page playwright.Page, objectID string) {
	dir := s.manager.ScreenshotsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("failed to create screenshots dir: %v", err)
		return
	}
	ts := time.Now().Format("20060102_150405")
	path := filepath.Join(dir, fmt.Sprintf("%s_%s.png", ts, objectID))
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path:     playwright.String(path),
		FullPage: playwright.Bool(true),
	}); err != nil {
		log.Printf("failed to save screenshot: %v", err)
		return
	}
	log.Printf("debug screenshot saved: %s", path)
}

// ---------------------------------------------------------------------------
// HTML detection helpers (same logic as HTTP registrar)
// ---------------------------------------------------------------------------

func hasSMSForm(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "sms_code") ||
		strings.Contains(lower, "смс-код") ||
		strings.Contains(lower, "введите код")
}

func isSuccess(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "регистрация завершена") ||
		strings.Contains(lower, "зарегистрированы") ||
		strings.Contains(lower, "успешно зарегистрирован")
}

func isAlreadyRegistered(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "уже зарегистрирован") ||
		strings.Contains(lower, "already registered")
}
