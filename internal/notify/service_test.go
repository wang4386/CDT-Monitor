package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wang4386/CDT-Monitor/internal/domain"
)

func TestReplacementsExposeWebhookVariables(t *testing.T) {
	event := domain.NotificationEvent{
		Type:      "threshold",
		Title:     "流量阈值告警",
		Summary:   "即将达到阈值",
		AccountID: 42,
		Fields: map[string]string{
			"当前流量": "12.3456 GB",
			"设定阈值": "95%",
			"实例":   "i-test",
			"实例状态": "Running",
		},
		CreatedAt: time.Date(2026, 7, 23, 8, 9, 10, 0, time.FixedZone("CST", 8*60*60)),
	}
	values := replacements(event)
	for key, want := range map[string]string{
		"#TITLE#": "流量阈值告警", "#MSG#": "即将达到阈值", "#ACCOUNT#": "42", "#ACCOUNT_ID#": "42",
		"#TRAFFIC#": "12.3456", "#MAX_TRAFFIC#": "95", "#INSTANCE#": "i-test", "#STATUS#": "Running", "#TYPE#": "threshold",
	} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q", key, values[key], want)
		}
	}
	if values["#CREATED_AT#"] != "2026-07-23T00:09:10Z" {
		t.Fatalf("#CREATED_AT# = %q", values["#CREATED_AT#"])
	}
}

func TestDingTalkWebhookAddsSignature(t *testing.T) {
	var timestamp, signature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamp = r.URL.Query().Get("timestamp")
		signature = r.URL.Query().Get("sign")
		if timestamp == "" || signature == "" {
			t.Error("DingTalk signature query parameters are missing")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := New()
	service.httpClient = server.Client()
	secret := "SEC-test"
	err := service.sendWebhook(context.Background(), domain.WebhookConfig{
		Enabled: true, Provider: "dingtalk", Secret: secret, URL: server.URL, Method: "POST", Type: "JSON",
		Body: `{"msgtype":"text","text":{"content":"#MSG#"}}`,
	}, domain.NotificationEvent{Title: "标题", Summary: "消息", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil {
		t.Fatalf("invalid timestamp %q: %v", timestamp, err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "\n" + secret))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if signature != want {
		t.Fatalf("signature = %q, want %q", signature, want)
	}
}

func TestEncodeBase64MIMEWrapsLinesAndRoundTrips(t *testing.T) {
	body := renderEmail(domain.NotificationEvent{
		Title:   "通知通道测试",
		Summary: "CDT Monitor 的通知配置工作正常。",
		Fields:  map[string]string{"发送时间": "2026-11-11 21:32:20"},
	})
	encoded := encodeBase64MIME(body)
	for _, line := range strings.Split(strings.TrimSuffix(encoded, "\r\n"), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("encoded line is %d characters, want at most 76", len(line))
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(encoded, "\r\n", ""))
	if err != nil {
		t.Fatalf("encoded body does not decode: %v", err)
	}
	if string(decoded) != body {
		t.Fatalf("decoded body does not match the original HTML")
	}
}

func TestReplaceTemplateJSONAndForm(t *testing.T) {
	replacements := map[string]string{"#MSG#": "hello world", "#TITLE#": "通知"}
	if got := replaceTemplate("msg=#MSG#", replacements, true); got != "msg=hello+world" {
		t.Fatalf("form replacement = %q", got)
	}
	if got := replaceTemplate(`{"message":"#MSG#"}`, replacements, false); got != `{"message":"hello world"}` {
		t.Fatalf("json replacement = %q", got)
	}
}
