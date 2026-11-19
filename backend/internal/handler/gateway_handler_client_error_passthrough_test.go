package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleFailoverExhausted_AnthropicClientErrorPassthroughRaw(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Unexpected value(s) for the anthropic-beta header. Please consult our documentation at docs.claude.com"}}`)
	h := &GatewayHandler{}
	h.handleFailoverExhausted(c, &service.UpstreamFailoverError{StatusCode: http.StatusBadRequest, ResponseBody: body}, service.PlatformAnthropic, false)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, string(body), w.Body.String())
}

func TestHandleFailoverExhausted_AnthropicClientErrorStreamStartedKeepsOriginalMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err := c.Writer.Write([]byte(": ping\n\n"))
	require.NoError(t, err)

	body := []byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	h := &GatewayHandler{}
	h.handleFailoverExhausted(c, &service.UpstreamFailoverError{StatusCode: 529, ResponseBody: body}, service.PlatformAnthropic, true)

	assert.Contains(t, w.Body.String(), `"type":"overloaded_error"`)
	assert.Contains(t, w.Body.String(), `"message":"Overloaded"`)
}

func TestHandleFailoverExhausted_AnthropicAuthErrorStillWrapped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked."}}`)
	h := &GatewayHandler{}
	h.handleFailoverExhausted(c, &service.UpstreamFailoverError{StatusCode: http.StatusUnauthorized, ResponseBody: body}, service.PlatformAnthropic, false)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.NotContains(t, w.Body.String(), "revoked")
}
