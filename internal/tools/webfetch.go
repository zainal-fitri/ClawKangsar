package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultUserAgent = "Mozilla/5.0 (X11; Linux armv7l) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

type WebFetcher struct {
	logger  *slog.Logger
	timeout time.Duration
	maxChars int
	client  *http.Client
}

func NewWebFetcher(logger *slog.Logger, timeout time.Duration, maxChars int) *WebFetcher {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if maxChars <= 0 {
		maxChars = 4000
	}

	return &WebFetcher{
		logger:   logger,
		timeout:  timeout,
		maxChars: maxChars,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:       4,
				IdleConnTimeout:    20 * time.Second,
				DisableCompression: false,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				return nil
			},
		},
	}
}

func (w *WebFetcher) Fetch(ctx context.Context, rawURL string) (string, error) {
	target, err := normalizeWebURL(rawURL)
	if err != nil {
		return "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch %s: status %d", target, resp.StatusCode)
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	text := string(payload)
	if strings.Contains(contentType, "text/html") || looksLikeHTML(text) {
		text = stripHTML(text)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("fetch %s: empty body text", target)
	}

	if len(text) > w.maxChars {
		text = text[:w.maxChars]
	}

	w.logger.Debug("web_fetch completed", "url", target, "chars", len(text))
	return text, nil
}

func normalizeWebURL(rawURL string) (string, error) {
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

func looksLikeHTML(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	return strings.HasPrefix(trimmed, "<!doctype") || strings.HasPrefix(trimmed, "<html")
}

func stripHTML(htmlContent string) string {
	reScript := regexp.MustCompile(`(?is)<script[\s\S]*?</script>`)
	reStyle := regexp.MustCompile(`(?is)<style[\s\S]*?</style>`)
	reTag := regexp.MustCompile(`(?is)<[^>]+>`)
	reMultiSpace := regexp.MustCompile(`[^\S\n]+`)
	reMultiLine := regexp.MustCompile(`\n{3,}`)

	result := reScript.ReplaceAllString(htmlContent, "")
	result = reStyle.ReplaceAllString(result, "")
	result = reTag.ReplaceAllString(result, "\n")
	result = reMultiSpace.ReplaceAllString(result, " ")
	result = reMultiLine.ReplaceAllString(result, "\n\n")

	lines := strings.Split(result, "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			clean = append(clean, line)
		}
	}

	return strings.Join(clean, "\n")
}
