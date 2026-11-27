package feishu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hook = regexp.MustCompile(`^https://open\.feishu\.cn/open-apis/bot/v2/hook/[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

func ValidWebhook(url string) bool { return hook.MatchString(url) }

type Client struct{ HTTP *http.Client }

func (c Client) Send(ctx context.Context, url, title, body string) error {
	payload := map[string]any{"msg_type": "post", "content": map[string]any{"post": map[string]any{"zh_cn": buildPost(title, body)}}}
	return c.sendPayload(ctx, url, payload)
}

func (c Client) sendPayload(ctx context.Context, url string, payload map[string]any) error {
	if !ValidWebhook(url) {
		return errors.New("invalid Feishu webhook")
	}
	if secret := os.Getenv("FEISHU_BOT_SIGNING_SECRET"); secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(ts+"\n"+secret))
		payload["timestamp"] = ts
		payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid Feishu request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("Feishu transport error")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Feishu HTTP %d", resp.StatusCode)
	}
	var result struct {
		Code *int `json:"code"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&result) != nil || result.Code == nil {
		return errors.New("invalid Feishu response")
	}
	if *result.Code != 0 {
		return fmt.Errorf("Feishu API code %d", *result.Code)
	}
	return nil
}

type element struct {
	Tag  string `json:"tag"`
	Text string `json:"text"`
	Href string `json:"href,omitempty"`
}
type post struct {
	Title   string      `json:"title"`
	Content [][]element `json:"content"`
}

func plain(s string) element { return element{Tag: "text", Text: s} }
func buildPost(title, body string) post {
	p := post{Title: title}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "|---|---|" || line == "| 项目 | 内容 |" || line == "| 指标 | 数值 |" {
			continue
		}
		var row []element
		switch {
		case strings.HasPrefix(line, "### "):
			heading := strings.TrimPrefix(line, "### ")
			if strings.HasSuffix(heading, " 内容审计") {
				heading = "审计概况 · " + strings.TrimSuffix(heading, " 内容审计")
			}
			if heading == "推送" {
				heading = "推送统计"
			}
			row = []element{plain("【" + heading + "】")}
		case strings.HasPrefix(line, "| ") && strings.HasSuffix(line, " |"):
			label, value, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | ")
			if ok {
				row = []element{plain(label + "："), plain(value)}
			}
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, ")"):
			label, href, ok := strings.Cut(line[1:len(line)-1], "](")
			u, err := url.Parse(href)
			if ok && err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") {
				if label == "在后台查看" {
					label = "查看审计记录"
				}
				row = []element{{Tag: "a", Text: label, Href: href}}
			}
		case strings.HasPrefix(line, "- **"):
			name, detail, ok := strings.Cut(strings.TrimPrefix(line, "- **"), "**")
			if ok {
				row = []element{plain(name), plain(detail)}
			}
		case strings.HasPrefix(line, "- "):
			label, value, ok := strings.Cut(strings.TrimPrefix(line, "- "), "：")
			if ok {
				row = []element{plain(label + "："), plain(value)}
			}
		}
		if len(row) == 0 {
			row = []element{plain(line)}
		}
		p.Content = append(p.Content, row)
	}
	return p
}
