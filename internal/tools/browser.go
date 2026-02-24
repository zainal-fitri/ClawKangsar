package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	defaultIdleTimeout = 5 * time.Minute
	defaultNavTimeout  = 45 * time.Second
	maxBodySize        = 4000
)

type Browser struct {
	logger *slog.Logger

	idleTimeout time.Duration

	mu           sync.Mutex
	allocCancel  context.CancelFunc
	browserCancel context.CancelFunc
	browserCtx   context.Context
	lastUsed     time.Time

	watchdogStop chan struct{}
	watchdogDone chan struct{}
}

type BrowserStats struct {
	Active             bool      `json:"active"`
	LastUsed           time.Time `json:"last_used,omitempty"`
	IdleTimeoutSeconds int       `json:"idle_timeout_seconds"`
}

func NewBrowser(logger *slog.Logger, idleTimeout time.Duration) *Browser {
	if logger == nil {
		logger = slog.Default()
	}
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleTimeout
	}

	b := &Browser{
		logger:       logger,
		idleTimeout:  idleTimeout,
		watchdogStop: make(chan struct{}),
		watchdogDone: make(chan struct{}),
	}
	go b.watchdogLoop()
	return b
}

func (b *Browser) Stats() BrowserStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	stats := BrowserStats{
		Active:             b.browserCtx != nil,
		IdleTimeoutSeconds: int(b.idleTimeout.Seconds()),
	}
	if !b.lastUsed.IsZero() {
		stats.LastUsed = b.lastUsed
	}
	return stats
}

func (b *Browser) Browse(ctx context.Context, rawURL string) (string, error) {
	target, err := normalizeURL(rawURL)
	if err != nil {
		return "", err
	}

	runCtx, err := b.acquireBrowser()
	if err != nil {
		return "", err
	}

	navCtx, cancel := context.WithTimeout(runCtx, defaultNavTimeout)
	defer cancel()

	var body string
	err = chromedp.Run(navCtx,
		chromedp.Navigate(target),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &body),
	)
	b.touch()
	if err != nil {
		return "", fmt.Errorf("browse %s: %w", target, err)
	}

	body = strings.TrimSpace(body)
	if len(body) > maxBodySize {
		body = body[:maxBodySize]
	}

	return body, nil
}

func (b *Browser) KillIfIdle() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.browserCtx == nil {
		return false
	}
	if time.Since(b.lastUsed) < b.idleTimeout {
		return false
	}

	b.killLocked("idle timeout")
	return true
}

func (b *Browser) Close() error {
	select {
	case <-b.watchdogDone:
	default:
		close(b.watchdogStop)
		<-b.watchdogDone
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.killLocked("shutdown")
	return nil
}

func (b *Browser) acquireBrowser() (context.Context, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.browserCtx != nil {
		b.lastUsed = time.Now()
		return b.browserCtx, nil
	}

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(browserCtx); err != nil {
		browserCancel()
		allocCancel()
		return nil, fmt.Errorf("start browser: %w", err)
	}

	b.allocCancel = allocCancel
	b.browserCancel = browserCancel
	b.browserCtx = browserCtx
	b.lastUsed = time.Now()
	b.logger.Info("browser started")

	return b.browserCtx, nil
}

func (b *Browser) touch() {
	b.mu.Lock()
	b.lastUsed = time.Now()
	b.mu.Unlock()
}

func (b *Browser) killLocked(reason string) {
	if b.browserCtx == nil {
		return
	}

	if b.browserCancel != nil {
		b.browserCancel()
		b.browserCancel = nil
	}
	if b.allocCancel != nil {
		b.allocCancel()
		b.allocCancel = nil
	}
	b.browserCtx = nil
	b.lastUsed = time.Time{}
	b.logger.Info("browser stopped", "reason", reason)
}

func (b *Browser) watchdogLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer close(b.watchdogDone)

	for {
		select {
		case <-b.watchdogStop:
			return
		case <-ticker.C:
			b.KillIfIdle()
		}
	}
}

func normalizeURL(rawURL string) (string, error) {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return "", errors.New("url is required")
	}

	if !strings.Contains(value, "://") {
		value = "https://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("only http/https urls are supported")
	}
	if parsed.Host == "" {
		return "", errors.New("url host is required")
	}

	return parsed.String(), nil
}
