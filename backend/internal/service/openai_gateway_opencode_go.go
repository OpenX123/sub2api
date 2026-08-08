package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func openCodeGoTLSProfile() *tlsfingerprint.Profile {
	return &tlsfingerprint.Profile{Name: "OpenCode Go (Node.js 24.x)"}
}

func isOpenCodeGoModelAccessError(account *Account, statusCode int, body []byte) bool {
	if account == nil || !account.IsOpenCodeGo() || statusCode != http.StatusForbidden {
		return false
	}
	errorType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error.type").String()))
	message := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error.message").String()))
	return errorType == "regionerror" ||
		strings.Contains(message, "not available in your region") ||
		strings.Contains(message, "requires explicit opt in")
}

func (s *OpenAIGatewayService) doOpenCodeGoAwareRequest(
	req *http.Request,
	proxyURL string,
	account *Account,
) (*http.Response, error) {
	if account != nil && account.IsOpenCodeGo() {
		return s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, openCodeGoTLSProfile())
	}
	return s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
}

// forwardOpenCodeGoMessages preserves the native Anthropic Messages wire
// format used by MiniMax and Qwen on OpenCode Go. Converting these requests to
// OpenAI Responses or Chat Completions changes tool/caching semantics and is
// rejected by the official Go endpoint.
func (s *OpenAIGatewayService) forwardOpenCodeGoMessages(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*OpenAIForwardResult, error) {
	startedAt := time.Now()
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	stream := gjson.GetBytes(body, "stream").Bool()
	baseURL, err := s.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil {
		return nil, fmt.Errorf("invalid OpenCode Go base URL: %w", err)
	}
	targetURL := strings.TrimRight(baseURL, "/") + "/v1/messages"

	upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, stream)
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build OpenCode Go Messages request: %w", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", account.GetOpenAIApiKey())
	req.Header.Set("anthropic-version", "2023-06-01")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.doOpenCodeGoAwareRequest(req, proxyURL, account)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if failoverErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, model); failoverErr != nil {
			return nil, failoverErr
		}
		if s.responseHeaderFilter != nil {
			responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		}
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
		return nil, fmt.Errorf("OpenCode Go Messages upstream returned HTTP %d", resp.StatusCode)
	}

	result := &OpenAIForwardResult{
		RequestID:        resp.Header.Get("x-request-id"),
		Model:            model,
		BillingModel:     model,
		UpstreamModel:    model,
		UpstreamEndpoint: "/v1/messages",
		Stream:           stream,
		ResponseHeaders:  resp.Header.Clone(),
	}
	SetActualOpenAIUpstreamEndpoint(c, result.UpstreamEndpoint)

	if !stream {
		responseBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("read OpenCode Go Messages response: %w", readErr)
		}
		result.Usage = openCodeGoMessagesUsage(responseBody)
		result.ResponseID = gjson.GetBytes(responseBody, "id").String()
		if responseModel := strings.TrimSpace(gjson.GetBytes(responseBody, "model").String()); responseModel != "" {
			result.UpstreamResponseModel = responseModel
		}
		if s.responseHeaderFilter != nil {
			responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		}
		c.Data(http.StatusOK, resp.Header.Get("Content-Type"), responseBody)
		result.Duration = time.Since(startedAt)
		return result, nil
	}

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	scanner := s.newUpstreamSSEScanner(resp.Body)
	var firstTokenAt time.Time
	for scanner.Scan() {
		line := scanner.Bytes()
		if _, writeErr := c.Writer.Write(append(append([]byte(nil), line...), '\n')); writeErr != nil {
			result.ClientDisconnect = true
			break
		}
		c.Writer.Flush()
		if firstTokenAt.IsZero() && len(bytes.TrimSpace(line)) > 0 {
			firstTokenAt = time.Now()
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			usage := openCodeGoMessagesUsage(data)
			mergeOpenCodeGoMessagesUsage(&result.Usage, usage)
			if id := gjson.GetBytes(data, "message.id").String(); id != "" {
				result.ResponseID = id
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && !result.ClientDisconnect {
		return result, fmt.Errorf("read OpenCode Go Messages stream: %w", scanErr)
	}
	if !firstTokenAt.IsZero() {
		firstTokenMs := int(firstTokenAt.Sub(startedAt).Milliseconds())
		result.FirstTokenMs = &firstTokenMs
	}
	result.Duration = time.Since(startedAt)
	return result, nil
}

func openCodeGoMessagesUsage(body []byte) OpenAIUsage {
	usagePath := "usage"
	if gjson.GetBytes(body, "message.usage").Exists() {
		usagePath = "message.usage"
	}
	return OpenAIUsage{
		InputTokens:              int(gjson.GetBytes(body, usagePath+".input_tokens").Int()),
		OutputTokens:             int(gjson.GetBytes(body, usagePath+".output_tokens").Int()),
		CacheCreationInputTokens: int(gjson.GetBytes(body, usagePath+".cache_creation_input_tokens").Int()),
		CacheReadInputTokens:     int(gjson.GetBytes(body, usagePath+".cache_read_input_tokens").Int()),
	}
}

func mergeOpenCodeGoMessagesUsage(dst *OpenAIUsage, src OpenAIUsage) {
	if src.InputTokens > 0 {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens > 0 {
		dst.OutputTokens = src.OutputTokens
	}
	if src.CacheCreationInputTokens > 0 {
		dst.CacheCreationInputTokens = src.CacheCreationInputTokens
	}
	if src.CacheReadInputTokens > 0 {
		dst.CacheReadInputTokens = src.CacheReadInputTokens
	}
}
