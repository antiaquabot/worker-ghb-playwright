package browser

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/playwright-community/playwright-go"
)

// Manager manages Chromium lifecycle: installation and access.
type Manager struct {
	cachePath string
	pw        *playwright.Playwright
}

func NewManager() *Manager {
	return &Manager{
		cachePath: defaultCachePath(),
	}
}

// EnsureInstalled checks if Chromium is cached; downloads it if not.
func (m *Manager) EnsureInstalled(ctx context.Context) error {
	if err := os.MkdirAll(m.cachePath, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	log.Printf("Проверка Chromium в кэше: %s", m.cachePath)

	runOptions := &playwright.RunOptions{
		DriverDirectory:  m.cachePath,
		BrowsersPath:     m.cachePath,
		SkipInstallBrowsers: false,
	}

	if err := playwright.Install(runOptions); err != nil {
		return fmt.Errorf("install playwright/chromium: %w", err)
	}
	log.Println("Chromium готов к использованию")
	return nil
}

// ForceUpdate re-downloads Chromium regardless of cache state.
func (m *Manager) ForceUpdate(ctx context.Context) error {
	// Remove existing cache to force re-download
	if err := os.RemoveAll(filepath.Join(m.cachePath, "chromium*")); err != nil {
		log.Printf("warn: cleanup failed: %v", err)
	}
	return m.EnsureInstalled(ctx)
}

// Launch starts a new headless Chromium browser instance.
func (m *Manager) Launch() (*playwright.Playwright, playwright.Browser, error) {
	pw, err := playwright.Run(&playwright.RunOptions{
		DriverDirectory:  m.cachePath,
		BrowsersPath:     m.cachePath,
		SkipInstallBrowsers: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start playwright: %w", err)
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		_ = pw.Stop()
		return nil, nil, fmt.Errorf("launch chromium: %w", err)
	}
	return pw, browser, nil
}

// ScreenshotsDir returns the directory for saving error screenshots.
func (m *Manager) ScreenshotsDir() string {
	return filepath.Join(m.cachePath, "screenshots")
}

// defaultCachePath returns the platform-specific cache directory.
func defaultCachePath() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "worker-ghb-playwright")
	default:
		// Linux, macOS
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		return filepath.Join(home, ".worker-ghb-playwright")
	}
}
