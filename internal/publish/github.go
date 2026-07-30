package publish

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/linvva/cf-node-bench/internal/model"
)

func (s *Service) PublishGitHub(ctx context.Context, summary model.RunSummary, settings Settings) model.PublicationResult {
	s.defaults()
	result := model.PublicationResult{Target: "github", State: "failed", StartedAt: s.Now()}
	content := FormatResults(summary.Results, settings.Output)
	if content == "" {
		result.State, result.Message, result.FinishedAt = "skipped", "没有可发布的节点", s.Now()
		return result
	}
	endpoint := s.githubContentURL(settings)
	headers := githubHeaders(settings.GitHub.Token)
	status, body, err := s.request(ctx, settings.Request, http.MethodGet, endpoint+"?ref="+url.QueryEscape(settings.GitHub.Branch), nil, headers, settings.GitHub.Token)
	if err != nil {
		result.Message, result.FinishedAt = err.Error(), s.Now()
		return result
	}
	sha := ""
	if status != http.StatusNotFound {
		var existing struct {
			SHA      string `json:"sha"`
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		if err := json.Unmarshal(body, &existing); err != nil {
			result.Message, result.FinishedAt = "GitHub 文件响应格式无效", s.Now()
			return result
		}
		sha = existing.SHA
		if existing.Encoding == "base64" {
			decoded, decodeErr := base64.StdEncoding.DecodeString(strings.ReplaceAll(existing.Content, "\n", ""))
			if decodeErr == nil && string(decoded) == content {
				result.State, result.Items, result.Message, result.FinishedAt = "succeeded", len(summary.Results), "内容未变化，无需提交", s.Now()
				return result
			}
		}
	}
	payload := map[string]any{
		"message": fmt.Sprintf("Update CF Node Bench results at %s", s.Now().UTC().Format(time.RFC3339)),
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  settings.GitHub.Branch,
	}
	if sha != "" {
		payload["sha"] = sha
	}
	status, body, err = s.request(ctx, settings.Request, http.MethodPut, endpoint, payload, headers, settings.GitHub.Token)
	if err != nil && status == http.StatusConflict {
		status, body, err = s.request(ctx, settings.Request, http.MethodGet, endpoint+"?ref="+url.QueryEscape(settings.GitHub.Branch), nil, headers, settings.GitHub.Token)
		if err == nil && status != http.StatusNotFound {
			var refreshed struct {
				SHA string `json:"sha"`
			}
			if json.Unmarshal(body, &refreshed) == nil && refreshed.SHA != "" {
				payload["sha"] = refreshed.SHA
				status, body, err = s.request(ctx, settings.Request, http.MethodPut, endpoint, payload, headers, settings.GitHub.Token)
			} else {
				err = fmt.Errorf("GitHub 文件响应缺少 SHA")
			}
		} else if err == nil {
			err = fmt.Errorf("GitHub 冲突后未找到目标文件")
		}
	}
	if err != nil {
		result.Message, result.FinishedAt = err.Error(), s.Now()
		return result
	}
	if status < 200 || status >= 300 || len(body) == 0 {
		result.Message, result.FinishedAt = "GitHub 更新失败", s.Now()
		return result
	}
	result.State, result.Items, result.Message, result.FinishedAt = "succeeded", len(summary.Results), "GitHub 文件已更新", s.Now()
	return result
}

func (s *Service) TestGitHub(ctx context.Context, settings Settings) error {
	endpoint := strings.TrimRight(s.GitHubBaseURL, "/") + "/repos/" + url.PathEscape(settings.GitHub.Owner) + "/" + url.PathEscape(settings.GitHub.Repository)
	status, _, err := s.request(ctx, settings.Request, http.MethodGet, endpoint, nil, githubHeaders(settings.GitHub.Token), settings.GitHub.Token)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return fmt.Errorf("GitHub 仓库不存在或 Token 无权访问")
	}
	return nil
}

func (s *Service) githubContentURL(settings Settings) string {
	segments := strings.Split(settings.GitHub.Path, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return strings.TrimRight(s.GitHubBaseURL, "/") + "/repos/" + url.PathEscape(settings.GitHub.Owner) + "/" + url.PathEscape(settings.GitHub.Repository) + "/contents/" + strings.Join(segments, "/")
}

func githubHeaders(token string) map[string]string {
	return map[string]string{
		"Accept": "application/vnd.github+json", "Authorization": "Bearer " + token,
		"X-GitHub-Api-Version": "2022-11-28", "User-Agent": "CF-Node-Bench",
	}
}
