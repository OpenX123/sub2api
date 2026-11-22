package routes

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Some forwarding paths never show the client's anthropic-beta to Anthropic:
// OAuth Claude Code mimicry rebuilds the beta list, Bedrock/Vertex filter it,
// and OpenAI/Grok/Antigravity bridges ignore it. On those paths a bogus beta
// would silently succeed, while Anthropic rejects it with a 400. This check
// restores that 400 for values that cannot be a real beta.
//
// It deliberately validates structure instead of keeping an allowlist: Claude
// Code ships new betas almost every release, and an allowlist rejected
// thousands of legitimate requests (per-turn-control-2026-07-01,
// mid-conversation-system-2026-04-07, ...). Every Anthropic beta so far is
// "<lowercase-name>-YYYY-MM-DD" or "<lowercase-name>-YYYYMMDD" (for example
// claude-code-20250219), dated no earlier than 2023. Well-formed but unknown
// names still pass here; on forwarding paths, Anthropic judges them itself.
var messagesBetaTokenPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*?-(\d{4}-\d{2}-\d{2}|\d{8})$`)

var messagesBetaEarliestDate = time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)

// Betas are sometimes dated shortly before their public launch. Leave a wide
// margin so only clearly fabricated future dates (such as 2099-01-01) fail.
const messagesBetaFutureTolerance = 180 * 24 * time.Hour

// messagesBetaNow is replaceable in tests.
var messagesBetaNow = time.Now

func isPlausibleMessagesBetaToken(token string, now time.Time) bool {
	match := messagesBetaTokenPattern.FindStringSubmatch(token)
	if match == nil {
		return false
	}
	dateText := match[1]
	layout := "2006-01-02"
	if !strings.Contains(dateText, "-") {
		layout = "20060102"
	}
	date, err := time.Parse(layout, dateText)
	if err != nil {
		return false
	}
	return !date.Before(messagesBetaEarliestDate) && !date.After(now.Add(messagesBetaFutureTolerance))
}

func validateMessagesBetaHeader(c *gin.Context) {
	now := messagesBetaNow()
	for _, value := range c.Request.Header.Values("Anthropic-Beta") {
		for token := range strings.SplitSeq(value, ",") {
			token = strings.TrimSpace(token)
			if token == "" || isPlausibleMessagesBetaToken(token, now) {
				continue
			}
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
			// Mirrors Anthropic's own error body, including the top-level
			// "type":"error" that other local errors in this package omit, so the
			// rejection looks the same as on paths that reach Anthropic.
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "invalid_request_error",
					"message": "Unexpected value(s) `" + token + "` for the `anthropic-beta` header. Please consult our documentation at platform.claude.com/docs or try again without the header.",
				},
			})
			return
		}
	}
	c.Next()
}
