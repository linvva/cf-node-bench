package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/linvva/cf-node-bench/internal/model"
)

type cloudflareRecord struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied *bool  `json:"proxied,omitempty"`
	Comment string `json:"comment"`
}

func (s *Service) PublishCloudflare(ctx context.Context, summary model.RunSummary, settings Settings) model.PublicationResult {
	s.defaults()
	started := s.Now()
	result := model.PublicationResult{Target: "cloudflare", State: "failed", RecordType: settings.Cloudflare.RecordType, StartedAt: started}
	contents, eligible, skipped := CloudflareContents(summary.Results, settings)
	result.Eligible, result.Skipped = eligible, skipped
	if len(contents) == 0 {
		if len(summary.Results) == 0 {
			result.State = "skipped"
		}
		result.Message = "没有可发布的节点，现有 DNS 记录未修改"
		result.FinishedAt = s.Now()
		return result
	}
	records, err := s.cloudflareManagedRecords(ctx, settings)
	if err != nil {
		result.Message = err.Error()
		result.FinishedAt = s.Now()
		return result
	}
	deletes := make([]map[string]string, 0, len(records))
	for _, record := range records {
		deletes = append(deletes, map[string]string{"id": record.ID})
	}
	posts := make([]cloudflareRecord, 0, len(contents))
	for _, content := range contents {
		record := cloudflareRecord{Name: settings.Cloudflare.RecordName, Type: settings.Cloudflare.RecordType, Content: content, TTL: settings.Cloudflare.TTL, Comment: managedComment}
		if settings.Cloudflare.RecordType == "A" {
			proxied := settings.Cloudflare.Proxied
			record.Proxied = &proxied
		}
		posts = append(posts, record)
	}
	payload := map[string]any{"deletes": deletes, "posts": posts}
	endpoint := strings.TrimRight(s.CloudflareBaseURL, "/") + "/zones/" + url.PathEscape(settings.Cloudflare.ZoneID) + "/dns_records/batch"
	status, body, err := s.request(ctx, settings.Request, http.MethodPost, endpoint, payload, cloudflareHeaders(settings.Cloudflare.APIToken), settings.Cloudflare.APIToken)
	if err == nil && status >= 200 && status < 300 {
		err = sanitize(successEnvelope(body), settings.Cloudflare.APIToken)
	}
	if err != nil {
		result.Message = err.Error()
		result.FinishedAt = s.Now()
		return result
	}
	result.State = "succeeded"
	result.Items = len(posts)
	result.Message = fmt.Sprintf("已写入 %d 条 %s 记录", len(posts), settings.Cloudflare.RecordType)
	result.FinishedAt = s.Now()
	return result
}

func (s *Service) cloudflareManagedRecords(ctx context.Context, settings Settings) ([]cloudflareRecord, error) {
	managed := []cloudflareRecord{}
	for _, recordType := range []string{"A", "TXT"} {
		page := 1
		for {
			query := url.Values{"type": {recordType}, "name": {settings.Cloudflare.RecordName}, "page": {strconv.Itoa(page)}, "per_page": {"100"}}
			endpoint := strings.TrimRight(s.CloudflareBaseURL, "/") + "/zones/" + url.PathEscape(settings.Cloudflare.ZoneID) + "/dns_records?" + query.Encode()
			status, body, err := s.request(ctx, settings.Request, http.MethodGet, endpoint, nil, cloudflareHeaders(settings.Cloudflare.APIToken), settings.Cloudflare.APIToken)
			if err != nil {
				return nil, err
			}
			var response struct {
				Success    bool               `json:"success"`
				Result     []cloudflareRecord `json:"result"`
				ResultInfo struct {
					TotalPages int `json:"total_pages"`
				} `json:"result_info"`
			}
			if status < 200 || status >= 300 || json.Unmarshal(body, &response) != nil || !response.Success {
				return nil, fmt.Errorf("查询 Cloudflare %s 记录失败", recordType)
			}
			for _, record := range response.Result {
				if record.Comment == managedComment {
					managed = append(managed, record)
				}
			}
			if response.ResultInfo.TotalPages <= page {
				break
			}
			page++
		}
	}
	return managed, nil
}

func (s *Service) TestCloudflare(ctx context.Context, settings Settings) error {
	_, err := s.cloudflareManagedRecords(ctx, settings)
	return err
}

func cloudflareHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}
