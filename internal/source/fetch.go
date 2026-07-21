package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HTTPSource struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Enabled     bool      `json:"enabled"`
	LastFetched time.Time `json:"lastFetched,omitempty"`
	LastStatus  string    `json:"lastStatus,omitempty"`
	NodeCount   int       `json:"nodeCount"`
}

type Fetcher struct {
	Client     *http.Client
	Timeout    time.Duration
	MaxRetries int
	RetryDelay time.Duration
}

func (f Fetcher) Fetch(ctx context.Context, source HTTPSource) (ParseResult, error) {
	var lastErr error
	retries := max(0, f.MaxRetries)
	for attempt := 0; attempt <= retries; attempt++ {
		result, err := f.fetchOnce(ctx, source)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ParseResult{}, ctx.Err()
		}
		if attempt == retries {
			break
		}
		delay := f.RetryDelay
		if delay <= 0 {
			delay = 300 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ParseResult{}, ctx.Err()
		}
	}
	return ParseResult{}, lastErr
}

func (f Fetcher) fetchOnce(ctx context.Context, source HTTPSource) (ParseResult, error) {
	client := f.Client
	if client == nil {
		timeout := f.Timeout
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return ParseResult{}, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return ParseResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ParseResult{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return ParseResult{}, fmt.Errorf("unsupported Content-Encoding %q", encoding)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return ParseResult{}, err
	}
	return Parse(body, source.ID), nil
}
