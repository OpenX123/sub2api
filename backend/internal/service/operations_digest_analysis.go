package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/feishu"
)

const digestAnalysisModel = "MiniMax-M3.1-Flash"

var (
	digestSpendTotal = regexp.MustCompile(`^今日消费[[:space:]　]+\$([0-9]+(?:\.[0-9]+)?)`)
	digestRiskCounts = regexp.MustCompile(`^🛡️ Jev[[:space:]　]+([0-9]+) 人命中 · ([0-9]+) 人重复拦截$`)
)

// Only explicitly selected aggregate metrics can leave the server. The detail
// rows, highlight and cost ranking contain user identifiers and email addresses.
func digestAnalysisMetrics(c *feishu.DigestCard) map[string]any {
	m := map[string]any{"本期三小时": c.AnalysisCurrent, "上一期三小时": c.AnalysisPrevious, "Claude可用与总数": c.Pool, "当前并发与容量": c.Concurrency, "号池状态": c.PoolDetail, "实时RPM": c.RPM, "首字P95": c.Latency, "耗时P95": c.Duration}
	if matches := digestSpendTotal.FindStringSubmatch(c.Spend); len(matches) == 2 {
		m["今日总消费美元"] = matches[1]
	}
	if matches := digestRiskCounts.FindStringSubmatch(c.Risk); len(matches) == 3 {
		m["三小时命中人数"] = matches[1]
		m["重复拦截人数"] = matches[2]
	}
	return m
}

func parseDigestSuggestions(text string) []string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json\n") && strings.HasSuffix(text, "```") {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "```json\n"), "```")
	}
	var lines []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &lines); err != nil {
		return nil
	}
	result := make([]string, 0, 2)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || utf8.RuneCountInString(line) > 80 || strings.ContainsAny(line, "\r\n") {
			continue
		}
		result = append(result, line)
		if len(result) == 2 {
			break
		}
	}
	return result
}

func (s *OpsService) digestAnalysis(ctx context.Context, card *feishu.DigestCard) []string {
	if s == nil || s.accountRepo == nil || s.concurrencyService == nil || s.openAIGatewayService == nil || s.openAIGatewayService.httpUpstream == nil {
		return nil
	}
	// Leave time for Feishu even when SQL gathering consumed most of the slot.
	budget := 10 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)-8*time.Second)
	}
	if budget <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	accounts, err := s.accountRepo.ListSchedulableByPlatform(ctx, PlatformMiniMax)
	if err != nil {
		return nil
	}
	for i := range accounts {
		a := &accounts[i]
		if !a.IsSchedulable() || !a.IsModelSupported(digestAnalysisModel) || a.GetCNAPIKey() == "" || a.ProxyID != nil && a.Proxy == nil {
			continue
		}
		req, err := s.digestModelRequest(ctx, a, card)
		if err != nil {
			continue
		}
		slot, err := s.concurrencyService.AcquireAccountSlot(ctx, a.ID, a.Concurrency)
		if err != nil || !slot.Acquired {
			continue
		}
		lines, retry := s.digestModelAttempt(ctx, a, req)
		go slot.ReleaseFunc()
		if !retry || ctx.Err() != nil {
			return lines
		}
	}
	return nil
}

// Return retry only when the request can reasonably succeed on a different account.
func (s *OpsService) digestModelAttempt(ctx context.Context, a *Account, req *http.Request) ([]string, bool) {
	proxy := ""
	if a.Proxy != nil {
		proxy = a.Proxy.URL()
	}
	resp, err := s.openAIGatewayService.httpUpstream.Do(req, proxy, a.ID, a.Concurrency)
	if err != nil {
		reason := "transport"
		if ctx.Err() != nil {
			reason = "timeout_or_canceled"
		}
		slog.Warn("operations_digest.analysis_failed", "reason", reason)
		return nil, ctx.Err() == nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("operations_digest.analysis_failed", "status", resp.StatusCode)
		return nil, resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
	}
	lines := readDigestAnalysis(resp.Body, a.IsAnthropicProtocol())
	if lines == nil {
		slog.Warn("operations_digest.analysis_failed", "reason", "invalid_response")
	}
	return lines, false
}

const digestSystemPrompt = "对比本期和上一期的运营聚合指标，给出最多两条有数据依据的中文观察或建议。只输出 JSON 字符串数组，每条不超过 60 字。没有明确发现时返回空数组。不要预测趋势或臆测故障原因。"

func (s *OpsService) digestModelRequest(ctx context.Context, a *Account, card *feishu.DigestCard) (*http.Request, error) {
	metrics, err := json.Marshal(digestAnalysisMetrics(card))
	if err != nil {
		return nil, err
	}
	user := map[string]string{"role": "user", "content": string(metrics)}
	body := map[string]any{"model": a.GetMappedModel(digestAnalysisModel), "stream": false, "max_tokens": 800}
	var base, endpoint string
	switch a.GetAPIProtocol() {
	case APIProtocolAnthropic:
		base = a.GetAnthropicProtocolBaseURL()
		endpoint = nativeAnthropicMessagesURL(a, base)
		body["system"] = digestSystemPrompt
		body["messages"] = []map[string]string{user}
	case APIProtocolChatCompletions, APIProtocolAdaptive:
		base = a.GetOpenAIFormatBaseURL()
		if a.IsAdaptiveAPIProtocol() {
			base = a.GetCNProtocolBaseURL(APIProtocolChatCompletions)
		}
		endpoint = buildOpenAIChatCompletionsURL(base)
		body["reasoning_split"] = true
		body["messages"] = []map[string]string{{"role": "system", "content": digestSystemPrompt}, user}
	default:
		return nil, fmt.Errorf("unsupported analysis protocol")
	}
	endpoint, err = cnValidateProbeURL(s.cfg, endpoint)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.IsAnthropicProtocol() {
		req.Header.Set("anthropic-version", "2023-06-01")
		setAnthropicAPIKeyAuthHeader(req.Header, a, a.GetCNAPIKey(), base)
	} else {
		req.Header.Set("Authorization", "Bearer "+a.GetCNAPIKey())
	}
	a.ApplyHeaderOverrides(req.Header)
	return req, nil
}

func readDigestAnalysis(body io.Reader, anthropic bool) []string {
	var parsed struct {
		Content []struct{ Type, Text string }
		Choices []struct{ Message struct{ Content string } }
	}
	if json.NewDecoder(io.LimitReader(body, 64<<10)).Decode(&parsed) != nil {
		return nil
	}
	if anthropic {
		var b strings.Builder
		for _, block := range parsed.Content {
			if block.Type == "text" {
				b.WriteString(block.Text)
			}
		}
		return parseDigestSuggestions(b.String())
	}
	if len(parsed.Choices) == 0 {
		return nil
	}
	return parseDigestSuggestions(parsed.Choices[0].Message.Content)
}
