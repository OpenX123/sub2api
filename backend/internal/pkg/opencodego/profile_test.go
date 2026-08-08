package opencodego

import "testing"

func TestIsBaseURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{DefaultBaseURL, true},
		{"https://opencode.ai/zen/go/v1", true},
		{"https://opencode.ai/zen/go/v1/models", true},
		{"https://opencode.ai/zen", false},
		{"https://example.com/zen/go", false},
	}
	for _, tt := range tests {
		if got := IsBaseURL(tt.url); got != tt.want {
			t.Fatalf("IsBaseURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestEndpointForModel(t *testing.T) {
	tests := map[string]Endpoint{
		"gpt-5.6-luna":      EndpointResponses,
		"minimax-m3":        EndpointMessages,
		"minimax-m2.5":      EndpointMessages,
		"qwen3.8-max":       EndpointMessages,
		"qwen3.5-plus":      EndpointMessages,
		"deepseek-v4-flash": EndpointChatCompletions,
		"kimi-k2.5":         EndpointChatCompletions,
		"hy3-preview":       EndpointChatCompletions,
	}
	for model, want := range tests {
		if got := EndpointForModel(model); got != want {
			t.Fatalf("EndpointForModel(%q) = %q, want %q", model, got, want)
		}
	}
}
