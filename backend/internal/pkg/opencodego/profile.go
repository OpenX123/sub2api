package opencodego

import (
	"net/url"
	"strings"
)

const DefaultBaseURL = "https://opencode.ai/zen/go"

type Endpoint string

const (
	EndpointChatCompletions Endpoint = "chat_completions"
	EndpointResponses       Endpoint = "responses"
	EndpointMessages        Endpoint = "messages"
)

// IsBaseURL reports whether a custom OpenAI-compatible base URL points at the
// official OpenCode Go API. Matching both host and path avoids treating the
// regular OpenCode Zen API as a Go subscription account.
func IsBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return false
	}
	path := strings.TrimRight(strings.ToLower(parsed.EscapedPath()), "/")
	return path == "/zen/go" || strings.HasPrefix(path, "/zen/go/v1")
}

// EndpointForModel follows the native endpoint matrix published in the
// OpenCode Go documentation. The live /v1/models response is authoritative for
// availability; this function only selects the wire protocol for a model.
func EndpointForModel(model string) Endpoint {
	id := strings.ToLower(strings.TrimSpace(model))
	if id == "gpt-5.6-luna" {
		return EndpointResponses
	}
	if strings.HasPrefix(id, "minimax-m") || strings.HasPrefix(id, "qwen3.") {
		return EndpointMessages
	}
	return EndpointChatCompletions
}
