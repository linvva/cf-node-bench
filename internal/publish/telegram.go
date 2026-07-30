package publish

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/linvva/cf-node-bench/internal/model"
)

func (s *Service) PublishTelegram(ctx context.Context, summary model.RunSummary, settings Settings, outcomes map[string]model.PublicationResult) model.PublicationResult {
	s.defaults()
	result := model.PublicationResult{Target: "telegram", State: "failed", StartedAt: s.Now()}
	messages := []string{telegramSummary(summary, settings, outcomes)}
	if settings.Telegram.ContentMode == "details" && len(summary.Results) > 0 {
		lines := make([]string, len(summary.Results))
		for index, item := range summary.Results {
			lines[index] = FormatResult(item, settings.Output)
		}
		chunks := chunkLines(lines, 3900)
		for index, chunk := range chunks {
			messages = append(messages, fmt.Sprintf("节点列表 (%d/%d)\n%s", index+1, len(chunks), chunk))
		}
	}
	for _, message := range messages {
		if err := s.telegramRequest(ctx, settings, "sendMessage", map[string]any{"chat_id": settings.Telegram.ChatID, "text": message}); err != nil {
			result.Message, result.FinishedAt = err.Error(), s.Now()
			return result
		}
	}
	result.State, result.Items, result.Message, result.FinishedAt = "succeeded", len(summary.Results), fmt.Sprintf("已发送 %d 条消息", len(messages)), s.Now()
	return result
}

func (s *Service) TestTelegram(ctx context.Context, settings Settings) error {
	return s.telegramRequest(ctx, settings, "getChat", map[string]any{"chat_id": settings.Telegram.ChatID})
}

func (s *Service) telegramRequest(ctx context.Context, settings Settings, method string, payload any) error {
	endpoint := strings.TrimRight(s.TelegramBaseURL, "/") + "/bot" + settings.Telegram.BotToken + "/" + method
	status, body, err := s.request(ctx, settings.Request, http.MethodPost, endpoint, payload, nil, settings.Telegram.BotToken)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("Telegram HTTP %d", status)
	}
	return sanitize(successEnvelope(body), settings.Telegram.BotToken)
}

func telegramSummary(summary model.RunSummary, settings Settings, outcomes map[string]model.PublicationResult) string {
	duration := summary.FinishedAt.Sub(summary.StartedAt)
	lines := []string{
		"CF Node Bench 测速完成",
		fmt.Sprintf("耗时：%.1f 秒", duration.Seconds()),
		fmt.Sprintf("通过节点：%d", len(summary.Results)),
		"Cloudflare：" + targetSummary("cloudflare", settings.Cloudflare.Enabled, outcomes),
		"GitHub：" + targetSummary("github", settings.GitHub.Enabled, outcomes),
	}
	return strings.Join(lines, "\n")
}

func targetSummary(target string, enabled bool, outcomes map[string]model.PublicationResult) string {
	if !enabled {
		return "未启用"
	}
	result, ok := outcomes[target]
	if !ok {
		return "未执行"
	}
	switch result.State {
	case "succeeded":
		return fmt.Sprintf("成功（%d 条）", result.Items)
	case "skipped":
		return "已跳过（" + result.Message + "）"
	default:
		return "失败（" + result.Message + "）"
	}
}

func chunkLines(lines []string, limit int) []string {
	chunks := []string{}
	current := ""
	for _, line := range lines {
		addition := line
		if current != "" {
			addition = "\n" + line
		}
		if current != "" && utf8.RuneCountInString(current+addition) > limit {
			chunks = append(chunks, current)
			current = line
		} else {
			current += addition
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}
