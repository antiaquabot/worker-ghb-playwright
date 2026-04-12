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

// Timeouts mirror Python worker constants.
const (
	navTimeout     = 30 * time.Second // REGISTER_VIA_PLAYWRIGHT_TIMEOUT_MS
	smsWaitTimeout = 3 * time.Minute  // SMS_CODE_WAIT_TIMEOUT_MS
	errorWaitDelay = 1 * time.Second  // ERROR_WAIT_TIMEOUT_MS
	retryDelay     = 2 * time.Second  // RETRY_INTERVAL_MS
	retryMaxTime   = 60 * time.Second // RETRY_TIMEOUT_MS
	tempErrorText  = "Попробуйте позже"
)

// Scenario performs browser-based auto-registration on a developer's website.
type Scenario interface {
	// Execute runs the registration scenario for objectID.
	// smsCodeFn is called when the browser is on the SMS confirmation step —
	// it should block until the user provides the code.
	Execute(ctx context.Context, objectID string, regURL string, personalData config.PersonalData, smsCodeFn SMSCodeFunc) error
}

// SMSCodeFunc blocks until the user provides an SMS confirmation code.
type SMSCodeFunc func(ctx context.Context) (string, error)

// GHBScenario implements Scenario for GHB via Playwright/Chromium.
//
// Registration flow mirrors Python's auto_registration_ui_worker._fill_and_submit_form:
//  1. Navigate (domcontentloaded)
//  2. Pre-form error check via .megaalerts
//  3. Wait for submit button, fill form with ID-selectors (#lastname, #firstname …)
//  4. Submit with retry on temporary "Попробуйте позже" server error
//  5. Wait for #sms_code to become visible (or detect immediate success)
//  6. Collect SMS code from user (Telegram reply or stdin)
//  7. Fill #sms_code, submit confirmation with the same retry logic
//  8. Final megaalerts check — no alert means success
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

	// Browser context with HTTPS errors ignored — mirrors Python: ignore_https_errors=True.
	bctx, err := br.NewContext(playwright.BrowserNewContextOptions{
		IgnoreHttpsErrors: playwright.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("new browser context: %w", err)
	}
	defer bctx.Close()

	page, err := bctx.NewPage()
	if err != nil {
		return fmt.Errorf("new page: %w", err)
	}

	// Screenshot on any error for debugging.
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
	// Step 1: Navigate — domcontentloaded mirrors Python's wait_until="domcontentloaded".
	// -----------------------------------------------------------------------
	log.Printf("[ghb-scenario] step 1: navigate to %s", regURL)
	if _, err := page.Goto(regURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(toMs),
	}); err != nil {
		return fmt.Errorf("step 1 navigate: %w", err)
	}

	// Pre-form megaalerts check (e.g. "already registered", service unavailable).
	if txt, hasErr := s.megaalertText(page); hasErr {
		return fmt.Errorf("step 1: server error: %s", txt)
	}

	// -----------------------------------------------------------------------
	// Step 2: Wait for form, fill fields.
	// ID-selectors (#lastname …) mirror Python's page.fill("#lastname", …).
	// -----------------------------------------------------------------------
	log.Printf("[ghb-scenario] step 2: waiting for submit button")
	if _, err := page.WaitForSelector(`form button[type=submit]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(toMs),
	}); err != nil {
		return fmt.Errorf("step 2: submit button not found: %w", err)
	}

	last, first, middle := pd.Parts()
	if last == "" || first == "" {
		return fmt.Errorf("personal_data: last_name and first_name are required")
	}
	phone := pd.PhoneDigits()
	if len(phone) != 9 {
		return fmt.Errorf("personal_data: phone must be 9 digits, got %q", phone)
	}

	log.Printf("[ghb-scenario] step 2: filling registration form")
	if err := page.Fill(`#lastname`, last); err != nil {
		return fmt.Errorf("fill lastname: %w", err)
	}
	if err := page.Fill(`#firstname`, first); err != nil {
		return fmt.Errorf("fill firstname: %w", err)
	}
	if middle != "" {
		if err := page.Fill(`#middlename`, middle); err != nil {
			log.Printf("[ghb-scenario] warn: middlename field: %v", err)
		}
	}
	if err := page.Fill(`#phone`, phone); err != nil {
		return fmt.Errorf("fill phone: %w", err)
	}

	consent := page.Locator(`#consent`)
	if n, _ := consent.Count(); n > 0 {
		if checked, _ := consent.IsChecked(); !checked {
			if err := consent.Check(); err != nil {
				log.Printf("[ghb-scenario] warn: consent checkbox: %v", err)
			}
		}
	}

	// -----------------------------------------------------------------------
	// Step 3: Submit form with retry on temporary server errors.
	// Mirrors Python's retry loop around page.click("form button[type=submit]").
	// -----------------------------------------------------------------------
	log.Printf("[ghb-scenario] step 3: submitting registration form")
	s.submitWithRetry(page, toMs)

	if txt, hasErr := s.megaalertText(page); hasErr {
		return fmt.Errorf("step 3: %s", txt)
	}

	// -----------------------------------------------------------------------
	// Step 4: Wait for #sms_code to become visible or detect immediate success.
	// Mirrors Python: sms_input_locator.wait_for(state="visible", timeout=…).
	// -----------------------------------------------------------------------
	smsLocator := page.Locator(`#sms_code`)
	smsFormPresent := smsLocator.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(toMs),
	}) == nil
	log.Printf("[ghb-scenario] SMS form present: %v", smsFormPresent)

	if !smsFormPresent {
		_, hasAlert := s.megaalertText(page)
		if s.hasSuccessHeading(page) && !hasAlert {
			log.Printf("[ghb-scenario] registration completed without SMS step for %s", objectID)
			return nil
		}
		return fmt.Errorf("step 4: registration outcome unclear — check %s manually", regURL)
	}

	// -----------------------------------------------------------------------
	// Step 5: Collect SMS code from user (Telegram reply or stdin).
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
	// Step 6: Fill SMS code and submit confirmation with the same retry logic.
	// -----------------------------------------------------------------------
	if err := page.Fill(`#sms_code`, code); err != nil {
		return fmt.Errorf("fill sms_code: %w", err)
	}

	log.Printf("[ghb-scenario] step 6: submitting SMS confirmation")
	s.submitWithRetry(page, toMs)

	if txt, hasErr := s.megaalertText(page); hasErr {
		return fmt.Errorf("step 6: SMS confirmation error: %s", txt)
	}

	log.Printf("[ghb-scenario] registration completed successfully for object %s", objectID)
	return nil
}

// submitWithRetry clicks the form submit button and retries while the server
// returns a temporary "Попробуйте позже" error.
// Mirrors Python's retry loop in _fill_and_submit_form.
func (s *GHBScenario) submitWithRetry(page playwright.Page, toMs float64) {
	deadline := time.Now().Add(retryMaxTime)
	for {
		log.Printf("[ghb-scenario] clicking submit button")
		if err := page.Click(`form button[type=submit]`); err != nil {
			log.Printf("[ghb-scenario] click submit (non-fatal): %v", err)
		}

		// Wait for page to settle after navigation (or stay on page if validation blocked).
		// Non-fatal: if no navigation happened the page is already in domcontentloaded.
		if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateDomcontentloaded,
			Timeout: playwright.Float(toMs),
		}); err != nil {
			log.Printf("[ghb-scenario] wait after submit (non-fatal): %v", err)
		}

		// Brief pause for server-rendered error messages — mirrors ERROR_WAIT_TIMEOUT_MS.
		time.Sleep(errorWaitDelay)

		txt, hasErr := s.megaalertText(page)
		if hasErr && strings.Contains(txt, tempErrorText) && time.Now().Before(deadline) {
			log.Printf("[ghb-scenario] temporary server error %q — retrying in %s", txt, retryDelay)
			time.Sleep(retryDelay)
			continue
		}
		break
	}
}

// megaalertText reads the first .megaalerts .megaalert-content element.
// Returns (text, true) when an alert is present, ("", false) otherwise.
// Mirrors Python: err_locator = page.locator(".megaalerts .megaalert-content").
func (s *GHBScenario) megaalertText(page playwright.Page) (string, bool) {
	loc := page.Locator(`.megaalerts .megaalert-content`)
	n, _ := loc.Count()
	if n == 0 {
		return "", false
	}
	txt, _ := loc.First().InnerText()
	return strings.TrimSpace(txt), true
}

// hasSuccessHeading returns true when the page contains "Регистрация завершена"
// in a heading or paragraph.
// Mirrors Python: page.locator("h1, h2, h3, p").filter(has_text="Регистрация завершена").
func (s *GHBScenario) hasSuccessHeading(page playwright.Page) bool {
	loc := page.Locator(`h1, h2, h3, p`).Filter(playwright.LocatorFilterOptions{
		HasText: "Регистрация завершена",
	})
	n, _ := loc.Count()
	return n > 0
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
