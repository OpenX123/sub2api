package service

import (
	"regexp"
	"strings"
)

const ClaudeSubscriptionTierExtraKey = "claude_subscription_tier"

var eduAccountName = regexp.MustCompile(`(?i)(^|[^a-z])edu([^a-z]|$)|教育`)

// ClaudeSubscriptionTier resolves the operator's label before legacy account naming.
func ClaudeSubscriptionTier(name string, extra map[string]any) string {
	if raw := extra[ClaudeSubscriptionTierExtraKey]; raw != nil && raw != "" {
		if value, ok := raw.(string); ok {
			switch tier := strings.ToLower(strings.TrimSpace(value)); tier {
			case "pro", "max", "ultra", "edu":
				return tier
			}
		}
		return "unknown"
	}
	if eduAccountName.MatchString(name) {
		return "edu"
	}
	return "unknown"
}
