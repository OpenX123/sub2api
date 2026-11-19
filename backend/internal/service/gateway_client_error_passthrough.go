package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

// accountLevelUpstreamErrorKeywords 命中这些关键词的上游错误描述的是网关自己的上游账号状态
// （组织停用、余额、实名、凭证范围），与客户端请求无关，不能原样暴露给客户。
var accountLevelUpstreamErrorKeywords = []string{
	"organization has been disabled",
	"credit balance",
	"identity verification",
}

// IsClientPassthroughUpstreamError 判断 Anthropic 上游错误能否把状态码和 body 原样返回给客户端。
//
// 原则：由客户端请求内容导致的错误（参数非法、未知 beta、模型不存在、请求过大、限流、
// 过载、上游 5xx）原样透传，不再包装成 "Upstream request failed" 之类的泛化文案；
// 描述网关上游账号凭证/权限/计费状态的错误（401/403、authentication/permission/billing
// 类型、组织停用、余额不足、实名、Claude Code 凭证范围）仍走原有的包装逻辑。
// body 必须是带 error.type 与 error.message 的 Anthropic 风格 JSON。
func IsClientPassthroughUpstreamError(statusCode int, body []byte) bool {
	if statusCode < 400 || statusCode == 401 || statusCode == 403 {
		return false
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	errType := strings.TrimSpace(gjson.GetBytes(body, "error.type").String())
	msg := strings.TrimSpace(gjson.GetBytes(body, "error.message").String())
	if errType == "" || msg == "" {
		return false
	}
	switch errType {
	case "authentication_error", "permission_error", "billing_error":
		return false
	}
	if isClaudeCodeCredentialScopeError(msg) {
		return false
	}
	lower := strings.ToLower(msg)
	for _, kw := range accountLevelUpstreamErrorKeywords {
		if strings.Contains(lower, kw) {
			return false
		}
	}
	return true
}
