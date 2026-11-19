package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsClientPassthroughUpstreamError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"unknown beta 400", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"Unexpected value(s) ` + "`zz-1`" + ` for the ` + "`anthropic-beta`" + ` header. Please consult our documentation at docs.claude.com or try again without the header."}}`, true},
		{"not found 404", 404, `{"type":"error","error":{"type":"not_found_error","message":"model: zz"}}`, true},
		{"too large 413", 413, `{"type":"error","error":{"type":"request_too_large","message":"Request exceeds the maximum size"}}`, true},
		{"rate limit 429", 429, `{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit."}}`, true},
		{"overloaded 529", 529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, true},
		{"api error 500", 500, `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`, true},
		{"401 kept wrapped", 401, `{"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked."}}`, false},
		{"403 kept wrapped", 403, `{"type":"error","error":{"type":"permission_error","message":"forbidden"}}`, false},
		{"billing type", 402, `{"type":"error","error":{"type":"billing_error","message":"pay"}}`, false},
		{"credit balance", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API."}}`, false},
		{"org disabled", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"This organization has been disabled."}}`, false},
		{"cc credential scope", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"This credential is only authorized for use with Claude Code and cannot be used for other API requests."}}`, false},
		{"missing type", 422, `{"error":{"message":"Invalid schema"}}`, false},
		{"not json", 502, `<html>bad gateway</html>`, false},
		{"success", 200, `{"type":"error","error":{"type":"x","message":"y"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsClientPassthroughUpstreamError(tc.status, []byte(tc.body)))
		})
	}
}

func TestGatewayHandleErrorResponse_AnthropicClientErrorPassthroughRaw(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	svc := &GatewayService{}
	respBody := []byte(`{"type":"error","error":{"type":"not_found_error","message":"model: claude-zz"},"request_id":"req_1"}`)
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Header:     http.Header{},
	}
	account := &Account{ID: 21, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	_, err := svc.handleErrorResponse(context.Background(), resp, c, account)
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, string(respBody), rec.Body.String())
}

func TestGatewayHandleRetryExhaustedError_AnthropicClientErrorPassthroughRaw(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	svc := &GatewayService{}
	respBody := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"speed: Input should be 'fast' or 'standard', got 'zz'"}}`)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Header:     http.Header{},
	}
	account := &Account{ID: 22, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}

	_, err := svc.handleRetryExhaustedError(context.Background(), resp, c, account)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, string(respBody), rec.Body.String())
}
