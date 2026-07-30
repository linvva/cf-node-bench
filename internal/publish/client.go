package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const managedComment = "Managed by CF Node Bench"

type Service struct {
	Client            *http.Client
	CloudflareBaseURL string
	GitHubBaseURL     string
	TelegramBaseURL   string
	Now               func() time.Time
}

func NewService() *Service {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Service{
		Client:            &http.Client{Transport: transport},
		CloudflareBaseURL: "https://api.cloudflare.com/client/v4",
		GitHubBaseURL:     "https://api.github.com",
		TelegramBaseURL:   "https://api.telegram.org",
		Now:               time.Now,
	}
}

func (s *Service) defaults() {
	if s.Client == nil {
		direct := NewService()
		s.Client = direct.Client
	}
	if s.CloudflareBaseURL == "" {
		s.CloudflareBaseURL = "https://api.cloudflare.com/client/v4"
	}
	if s.GitHubBaseURL == "" {
		s.GitHubBaseURL = "https://api.github.com"
	}
	if s.TelegramBaseURL == "" {
		s.TelegramBaseURL = "https://api.telegram.org"
	}
	if s.Now == nil {
		s.Now = time.Now
	}
}

func (s *Service) request(ctx context.Context, policy RequestPolicy, method, requestURL string, payload any, headers map[string]string, secrets ...string) (int, []byte, error) {
	s.defaults()
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
	}
	var lastStatus int
	var lastBody []byte
	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, time.Duration(policy.TimeoutMS)*time.Millisecond)
		req, err := http.NewRequestWithContext(attemptCtx, method, requestURL, bytes.NewReader(body))
		if err != nil {
			cancel()
			return 0, nil, sanitize(err, secrets...)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, requestErr := s.Client.Do(req)
		if requestErr != nil {
			cancel()
			if ctx.Err() != nil {
				return 0, nil, ctx.Err()
			}
			if attempt < policy.MaxRetries {
				if err := waitRetry(ctx, time.Duration(policy.RetryDelayMS)*time.Millisecond); err != nil {
					return 0, nil, err
				}
				continue
			}
			return 0, nil, sanitize(requestErr, secrets...)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		_ = resp.Body.Close()
		cancel()
		lastStatus, lastBody = resp.StatusCode, responseBody
		if readErr != nil {
			return resp.StatusCode, nil, sanitize(readErr, secrets...)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == http.StatusNotFound {
			return resp.StatusCode, responseBody, nil
		}
		if attempt < policy.MaxRetries && retryableStatus(resp.StatusCode) {
			delay := time.Duration(policy.RetryDelayMS) * time.Millisecond
			if seconds, parseErr := strconv.Atoi(resp.Header.Get("Retry-After")); parseErr == nil && seconds > 0 {
				delay = time.Duration(seconds) * time.Second
			}
			if err := waitRetry(ctx, delay); err != nil {
				return 0, nil, err
			}
			continue
		}
		return resp.StatusCode, responseBody, apiError(resp.StatusCode, responseBody, secrets...)
	}
	return lastStatus, lastBody, apiError(lastStatus, lastBody, secrets...)
}

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func apiError(status int, body []byte, secrets ...string) error {
	message := strings.TrimSpace(string(body))
	if len(message) > 300 {
		message = message[:300]
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return sanitize(fmt.Errorf("HTTP %d: %s", status, message), secrets...)
}

func sanitize(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	return fmt.Errorf("%s", message)
}

func successEnvelope(body []byte) error {
	var response struct {
		Success bool `json:"success"`
		OK      bool `json:"ok"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("响应不是有效 JSON: %w", err)
	}
	if response.Success || response.OK {
		return nil
	}
	if response.Description != "" {
		return fmt.Errorf("%s", response.Description)
	}
	if len(response.Errors) > 0 {
		return fmt.Errorf("%s", response.Errors[0].Message)
	}
	return fmt.Errorf("API 返回失败")
}
