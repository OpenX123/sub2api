package serverchan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidSendKey(t *testing.T) {
	require.True(t, ValidSendKey("SCT470902TabcDEF123"))
	require.False(t, ValidSendKey(""))
	require.False(t, ValidSendKey("sctp123t456"))
	require.False(t, ValidSendKey("SCT123/../x"))
}

func TestTruncateTitle(t *testing.T) {
	require.Equal(t, "a b", TruncateTitle("a\nb"))
	long := strings.Repeat("审", 40)
	require.Equal(t, strings.Repeat("审", 32), TruncateTitle(long))
}

func TestSendPostsFormAndChecksCode(t *testing.T) {
	var gotPath, gotTitle, gotDesp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		require.NoError(t, r.ParseForm())
		gotTitle, gotDesp = r.PostForm.Get("title"), r.PostForm.Get("desp")
		_, _ = w.Write([]byte(`{"code":0,"message":"","data":{"pushid":"1"}}`))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), EndpointFor: func(key string) string { return server.URL + "/" + key + ".send" }}

	require.NoError(t, client.Send(context.Background(), "SCTkey123", "标题\n换行", "**正文**"))
	require.Equal(t, "/SCTkey123.send", gotPath)
	require.Equal(t, "标题 换行", gotTitle)
	require.Equal(t, "**正文**", gotDesp)
}

func TestSendErrorsNeverContainKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":40001,"message":"bad key SCTkey123"}`))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), EndpointFor: func(key string) string { return server.URL + "/" + key + ".send" }}

	err := client.Send(context.Background(), "SCTkey123", "t", "d")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SCTkey123")
	require.Contains(t, err.Error(), "40001")

	require.ErrorIs(t, client.Send(context.Background(), "bad", "t", "d"), ErrInvalidSendKey)
}

func TestSendHTTPStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), EndpointFor: func(key string) string { return server.URL + "/" + key + ".send" }}
	require.ErrorContains(t, client.Send(context.Background(), "SCTkey123", "t", "d"), "429")
}
