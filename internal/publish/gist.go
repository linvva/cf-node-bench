package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/linvva/cf-node-bench/internal/model"
)

type gistResponse struct {
	ID    string              `json:"id"`
	Files map[string]gistFile `json:"files"`
}

type gistFile struct {
	Filename  string `json:"filename"`
	Content   string `json:"content"`
	RawURL    string `json:"raw_url"`
	Truncated bool   `json:"truncated"`
}

func (s *Service) PublishGist(ctx context.Context, summary model.RunSummary, settings Settings) model.PublicationResult {
	s.defaults()
	result := model.PublicationResult{Target: "gist", State: "failed", StartedAt: s.Now()}
	content := FormatResults(summary.Results, settings.Output)
	if content == "" {
		result.State, result.Message, result.FinishedAt = "skipped", "没有可发布的节点", s.Now()
		return result
	}

	endpoint := s.gistURL(settings.Gist.GistID)
	headers := githubHeaders(settings.Gist.Token)
	existing, status, err := s.getGist(ctx, settings, endpoint, headers)
	if err != nil {
		result.Message, result.FinishedAt = err.Error(), s.Now()
		return result
	}
	if status == http.StatusNotFound {
		result.Message, result.FinishedAt = "Gist 不存在或 Token 无权访问", s.Now()
		return result
	}
	if file, ok := findGistFile(existing.Files, settings.Gist.Filename); ok && !file.Truncated && file.Content == content {
		result.State, result.Items, result.URL, result.Message, result.FinishedAt = "succeeded", len(summary.Results), file.RawURL, "内容未变化，无需更新", s.Now()
		return result
	}

	payload := map[string]any{"files": map[string]any{settings.Gist.Filename: map[string]string{"content": content}}}
	status, body, err := s.request(ctx, settings.Request, http.MethodPatch, endpoint, payload, headers, settings.Gist.Token)
	if err != nil {
		result.Message, result.FinishedAt = err.Error(), s.Now()
		return result
	}
	if status < 200 || status >= 300 {
		result.Message, result.FinishedAt = fmt.Sprintf("Gist 更新失败：HTTP %d", status), s.Now()
		return result
	}
	var updated gistResponse
	if err := json.Unmarshal(body, &updated); err != nil || updated.Files == nil {
		result.Message, result.FinishedAt = "Gist 更新响应格式无效", s.Now()
		return result
	}
	file, ok := findGistFile(updated.Files, settings.Gist.Filename)
	if !ok {
		result.Message, result.FinishedAt = "Gist 更新响应缺少目标文件", s.Now()
		return result
	}
	result.State, result.Items, result.URL, result.Message, result.FinishedAt = "succeeded", len(summary.Results), file.RawURL, "Gist 文件已更新", s.Now()
	return result
}

func (s *Service) TestGist(ctx context.Context, settings Settings) error {
	s.defaults()
	response, status, err := s.getGist(ctx, settings, s.gistURL(settings.Gist.GistID), githubHeaders(settings.Gist.Token))
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return fmt.Errorf("Gist 不存在或 Token 无权访问")
	}
	if response.ID == "" || response.Files == nil {
		return fmt.Errorf("Gist 响应格式无效")
	}
	return nil
}

func (s *Service) getGist(ctx context.Context, settings Settings, endpoint string, headers map[string]string) (gistResponse, int, error) {
	status, body, err := s.request(ctx, settings.Request, http.MethodGet, endpoint, nil, headers, settings.Gist.Token)
	if err != nil || status == http.StatusNotFound {
		return gistResponse{}, status, err
	}
	var response gistResponse
	if err := json.Unmarshal(body, &response); err != nil || response.Files == nil {
		return gistResponse{}, status, fmt.Errorf("Gist 响应格式无效")
	}
	return response, status, nil
}

func (s *Service) gistURL(gistID string) string {
	return strings.TrimRight(s.GitHubBaseURL, "/") + "/gists/" + url.PathEscape(gistID)
}

func findGistFile(files map[string]gistFile, filename string) (gistFile, bool) {
	if file, ok := files[filename]; ok {
		return file, true
	}
	for _, file := range files {
		if file.Filename == filename {
			return file, true
		}
	}
	return gistFile{}, false
}
