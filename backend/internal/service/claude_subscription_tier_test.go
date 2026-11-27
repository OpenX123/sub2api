package service

import "testing"

func TestClaudeSubscriptionTier(t *testing.T) {
	for _, tc := range []struct{ name, label, want string }{
		{"Claude-EDU01", "", "edu"},
		{"a@school.edu", "", "edu"},
		{"教育号-3", "", "edu"},
		{"scheduled", "", "unknown"},
		{"Max", "", "unknown"},
		{"Claude-EDU01", "max", "max"},
		{"Claude-EDU01", "unknown", "unknown"},
		{"Claude-EDU01", "other", "unknown"},
	} {
		if got := ClaudeSubscriptionTier(tc.name, map[string]any{ClaudeSubscriptionTierExtraKey: tc.label}); got != tc.want {
			t.Errorf("name %q, label %q: got %q, want %q", tc.name, tc.label, got, tc.want)
		}
	}
}
