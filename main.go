package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/stroi-homes/worker-ghb-playwright/internal/browser"
	"github.com/stroi-homes/worker-ghb-playwright/internal/config"
	"github.com/stroi-homes/worker-ghb-playwright/internal/notifier"
	"github.com/stroi-homes/worker-ghb-playwright/internal/polling"
	"github.com/stroi-homes/worker-ghb-playwright/internal/scenario"
	"github.com/stroi-homes/worker-ghb-playwright/internal/sse"
	"github.com/stroi-homes/worker-ghb-playwright/internal/watchlist"
)

// DeveloperID is hardcoded at compile time — not read from config.
const DeveloperID = "ghb"

// Version is set by the build system via ldflags.
var Version = "dev"

func main() {
	var (
		configPath    = flag.String("config", "config.yaml", "path to config file")
		showVersion   = flag.Bool("version", false, "print version and exit")
		updateBrowser = flag.Bool("update-browser", false, "force update Chromium and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "worker-ghb-playwright %s\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  worker-ghb-playwright [flags]\n")
		fmt.Fprintf(os.Stderr, "  worker-ghb-playwright init --config config.yaml\n")
		fmt.Fprintf(os.Stderr, "  worker-ghb-playwright edit --config config.yaml\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("worker-ghb-playwright %s (developer: %s)\n", Version, DeveloperID)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subcommands
	if flag.NArg() > 0 {
		switch flag.Arg(0) {
		case "init":
			if err := config.InitConfig(*configPath); err != nil {
				log.Fatalf("init failed: %v", err)
			}
			return
		case "edit":
			if err := config.EditConfig(*configPath); err != nil {
				log.Fatalf("edit failed: %v", err)
			}
			return
		default:
			flag.Usage()
			os.Exit(1)
		}
	}

	// Ensure Chromium is installed
	bm := browser.NewManager()
	if *updateBrowser {
		log.Println("Обновление Chromium...")
		if err := bm.ForceUpdate(ctx); err != nil {
			log.Fatalf("browser update failed: %v", err)
		}
		log.Println("Chromium обновлён.")
		return
	}
	if err := bm.EnsureInstalled(ctx); err != nil {
		log.Fatalf("browser setup failed: %v", err)
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("worker-ghb-playwright %s started (developer_id=%s)", Version, DeveloperID)

	tgEnabled := cfg.Telegram.Enabled && cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != ""
	var tg *notifier.Notifier
	if tgEnabled {
		tg = notifier.New(cfg.Telegram.BotToken, cfg.Telegram.ChatID)
	}

	wl := watchlist.New(cfg.WatchList)
	reg := scenario.NewGHBScenario(bm)

	// minskLoc is UTC+3 (Europe/Minsk) — matches the Python worker's ZoneInfo("Europe/Minsk").
	minskLoc := time.FixedZone("Europe/Minsk", 3*60*60)

	// smsDeadlineStr formats the context deadline the same way as Python:
	// "[DD.MM.YYYY HH:MM:SS:cc]" where cc is hundredths of a second.
	smsDeadlineStr := func(ctx context.Context) string {
		dl, ok := ctx.Deadline()
		if !ok {
			return ""
		}
		dl = dl.In(minskLoc)
		cc := dl.Nanosecond() / 10_000_000
		return fmt.Sprintf("[%s:%02d]", dl.Format("02.01.2006 15:04:05"), cc)
	}

	var smsCodeFn func(innerCtx context.Context) (string, error)
	if tgEnabled {
		smsCodeFn = func(innerCtx context.Context) (string, error) {
			dl := smsDeadlineStr(innerCtx)
			msg := fmt.Sprintf(
				"📩 На ваш номер телефона отправлен код подтверждения.\n"+
					"Введите код из SMS до %s — иначе регистрация завершится с ошибкой.\n\n"+
					"Отправьте код ответным сообщением.",
				dl,
			)
			msgID, err := tg.SendWithMessageID(innerCtx, msg)
			if err != nil {
				log.Printf("telegram send error: %v", err)
				log.Printf("[sms-code] falling back to stdin...")
				fmt.Printf("Введите SMS-код до %s: ", dl)
				var code string
				if _, err := fmt.Scanln(&code); err != nil {
					return "", fmt.Errorf("read SMS code: %w", err)
				}
				return code, nil
			}
			log.Printf("[sms-code] waiting for SMS code via Telegram (reply to message %d)...", msgID)
			code, err := tg.WaitForCode(innerCtx, msgID)
			if err != nil {
				log.Printf("[sms-code] telegram wait error: %v, falling back to stdin", err)
				fmt.Printf("Введите SMS-код до %s: ", dl)
				var code string
				if _, err := fmt.Scanln(&code); err != nil {
					return "", fmt.Errorf("read SMS code: %w", err)
				}
				return code, nil
			}
			return code, nil
		}
	} else {
		smsCodeFn = func(innerCtx context.Context) (string, error) {
			fmt.Printf("Введите SMS-код до %s: ", smsDeadlineStr(innerCtx))
			var code string
			if _, err := fmt.Scanln(&code); err != nil {
				return "", fmt.Errorf("read SMS code: %w", err)
			}
			return code, nil
		}
	}

	var (
		activeRegsMu sync.Mutex
		activeRegs   = make(map[string]bool)
	)

	handler := func(eventType, externalID string, data map[string]any) {
		if eventType != "REGISTRATION_OPENED" {
			return
		}
		log.Printf("[event] %s: %s", eventType, externalID)

		regURL, _ := data["registration_url"].(string)
		entries := wl.Match(externalID)
		for _, entry := range entries {
			if entry.NotifyOnOpen {
				if tgEnabled {
					msg := tg.FormatRegistrationOpened(externalID, data)
					if err := tg.Send(ctx, msg); err != nil {
						log.Printf("telegram send error: %v", err)
					}
				} else {
					log.Printf("[notify] Регистрация открыта: %s", externalID)
				}
			}
			if entry.AutoRegister {
				activeRegsMu.Lock()
				if activeRegs[externalID] {
					activeRegsMu.Unlock()
					log.Printf("[dedup] registration already in progress for %s, skipping", externalID)
					break
				}
				activeRegs[externalID] = true
				activeRegsMu.Unlock()

				go func(eid string) {
					defer func() {
						activeRegsMu.Lock()
						delete(activeRegs, eid)
						activeRegsMu.Unlock()
					}()
					log.Printf("launching browser registration for object %s", eid)
					if err := reg.Execute(ctx, eid, regURL, cfg.PersonalData, smsCodeFn); err != nil {
						log.Printf("registration failed for %s: %v", eid, err)
						if tgEnabled {
							_ = tg.Send(ctx, tg.FormatRegistrationError(eid, err))
						}
					} else {
						if tgEnabled {
							_ = tg.Send(ctx, tg.FormatRegistrationSuccess(eid))
						}
					}
				}(externalID)
				break
			}
		}
	}

	// SSE with polling fallback and recovery
	if cfg.Service.UseSSE {
		sseClient := sse.New(cfg.Service.BaseURL, DeveloperID, handler)
		pollingClient := polling.New(cfg.Service.BaseURL, DeveloperID, cfg.Service.PollIntervalSeconds, handler)

		// sseRetryInterval: how long to stay in polling before retrying SSE.
		const sseRetryInterval = 5 * time.Minute

		go func() {
			for {
				if err := sseClient.Run(ctx); ctx.Err() != nil {
					return
				} else {
					log.Printf("SSE stopped (%v), switching to polling for %v", err, sseRetryInterval)
					if tgEnabled {
						_ = tg.Send(ctx, "⚠️ SSE недоступен, переключился на REST-поллинг")
					}
				}

				pollCtx, pollCancel := context.WithTimeout(ctx, sseRetryInterval)
				if err := pollingClient.Run(pollCtx); ctx.Err() != nil {
					pollCancel()
					return
				} else if err != nil {
					log.Printf("polling stopped: %v", err)
				}
				pollCancel()
				log.Printf("Retrying SSE after polling interval")
			}
		}()
	} else {
		pollingClient := polling.New(cfg.Service.BaseURL, DeveloperID, cfg.Service.PollIntervalSeconds, handler)
		go func() {
			if err := pollingClient.Run(ctx); err != nil {
				log.Printf("polling stopped: %v", err)
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	cancel()
}
