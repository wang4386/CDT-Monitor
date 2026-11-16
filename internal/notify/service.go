package notify

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/wang4386/CDT-Monitor/internal/domain"
)

type Service struct {
	httpClient *http.Client
}

func New() *Service {
	return &Service{httpClient: &http.Client{Timeout: 12 * time.Second}}
}

func EnabledChannels(config domain.Config) []string {
	channels := make([]string, 0, 3)
	if config.Notifications.Email.Enabled && config.Notifications.Email.To != "" {
		channels = append(channels, "email")
	}
	if config.Notifications.Telegram.Enabled && config.Notifications.Telegram.Token != "" && config.Notifications.Telegram.ChatID != "" {
		channels = append(channels, "telegram")
	}
	if config.Notifications.Webhook.Enabled && config.Notifications.Webhook.URL != "" {
		channels = append(channels, "webhook")
	}
	return channels
}

func (s *Service) Send(ctx context.Context, channel string, event domain.NotificationEvent, config domain.Config) error {
	switch channel {
	case "email":
		return sendEmail(ctx, config.Notifications.Email, event)
	case "telegram":
		return s.sendTelegram(ctx, config.Notifications.Telegram, event)
	case "webhook":
		return s.sendWebhook(ctx, config.Notifications.Webhook, event)
	default:
		return fmt.Errorf("unsupported notification channel %q", channel)
	}
}

func sendEmail(ctx context.Context, config domain.EmailConfig, event domain.NotificationEvent) error {
	if config.Host == "" || config.Port == 0 || config.Username == "" || config.To == "" {
		return errors.New("SMTP host, port, username and recipient are required")
	}
	hostPort := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var client *smtp.Client
	if strings.EqualFold(config.Security, "ssl") || config.Port == 465 {
		conn, err := tls.DialWithDialer(dialer, "tcp", hostPort, &tls.Config{ServerName: config.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return err
		}
		client, err = smtp.NewClient(conn, config.Host)
		if err != nil {
			conn.Close()
			return err
		}
	} else {
		conn, err := dialer.DialContext(ctx, "tcp", hostPort)
		if err != nil {
			return err
		}
		client, err = smtp.NewClient(conn, config.Host)
		if err != nil {
			conn.Close()
			return err
		}
		if strings.EqualFold(config.Security, "tls") || strings.EqualFold(config.Security, "starttls") {
			if err = client.StartTLS(&tls.Config{ServerName: config.Host, MinVersion: tls.VersionTLS12}); err != nil {
				client.Close()
				return err
			}
		}
	}
	defer client.Close()
	if config.Password != "" {
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(config.Username); err != nil {
		return err
	}
	if err := client.Rcpt(config.To); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	subject := mime.QEncoding.Encode("UTF-8", "CDT Monitor · "+event.Title)
	message := "From: CDT Monitor <" + config.Username + ">\r\n" +
		"To: " + config.To + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" + encodeBase64MIME(renderEmail(event))
	if _, err = io.WriteString(writer, message); err != nil {
		writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// encodeBase64MIME encodes the message body as base64 wrapped at 76
// characters per line, as required by RFC 2045. The HTML body is a single
// long line, so sending it raw exceeds the 998-octet line limit of
// RFC 5321 and strict SMTP servers (e.g. QQ Mail) reject it with
// "500 Line too long".
func encodeBase64MIME(body string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var builder strings.Builder
	for len(encoded) > 76 {
		builder.WriteString(encoded[:76])
		builder.WriteString("\r\n")
		encoded = encoded[76:]
	}
	builder.WriteString(encoded)
	builder.WriteString("\r\n")
	return builder.String()
}

func renderEmail(event domain.NotificationEvent) string {
	var rows strings.Builder
	for key, value := range event.Fields {
		rows.WriteString("<tr><td style=\"padding:12px 0;color:#8e8e93;border-bottom:1px solid #eee\">")
		rows.WriteString(html.EscapeString(key))
		rows.WriteString("</td><td style=\"padding:12px 0;text-align:right;font-weight:700;border-bottom:1px solid #eee\">")
		rows.WriteString(html.EscapeString(value))
		rows.WriteString("</td></tr>")
	}
	return `<!doctype html><html><body style="margin:0;background:#f2f2f7;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#1c1c1e"><table width="100%"><tr><td align="center" style="padding:40px 20px"><table width="100%" style="max-width:560px;background:rgba(255,255,255,.92);border:1px solid #fff;border-radius:28px;box-shadow:0 24px 48px -12px rgba(0,0,0,.08)"><tr><td style="padding:36px"><div style="font-size:11px;font-weight:800;letter-spacing:.16em;color:#6e6e73">CDT MONITOR</div><h1 style="font-size:26px;margin:10px 0">` + html.EscapeString(event.Title) + `</h1><p style="color:#6e6e73;white-space:pre-wrap;line-height:1.6">` + html.EscapeString(event.Summary) + `</p><table width="100%" style="margin-top:24px;border-top:1px solid #eee">` + rows.String() + `</table></td></tr></table></td></tr></table></body></html>`
}

func (s *Service) sendTelegram(ctx context.Context, config domain.TelegramConfig, event domain.NotificationEvent) error {
	baseURL := "https://api.telegram.org"
	if config.ProxyType == "custom" && config.ProxyURL != "" {
		baseURL = strings.TrimRight(config.ProxyURL, "/")
	}
	endpoint := baseURL + "/bot" + config.Token + "/sendMessage"
	form := url.Values{"chat_id": {config.ChatID}, "text": {eventText(event)}}
	client := s.httpClient
	if config.ProxyType == "socks5" && config.ProxyIP != "" && config.ProxyPort != "" {
		var auth *proxy.Auth
		if config.ProxyUser != "" || config.ProxyPass != "" {
			auth = &proxy.Auth{User: config.ProxyUser, Password: config.ProxyPass}
		}
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(config.ProxyIP, config.ProxyPort), auth, proxy.Direct)
		if err != nil {
			return err
		}
		client = &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(network, address)
		}}}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (s *Service) sendWebhook(ctx context.Context, config domain.WebhookConfig, event domain.NotificationEvent) error {
	replacements := replacements(event)
	endpoint := replaceTemplate(config.URL, replacements, true)
	if strings.EqualFold(config.Provider, "dingtalk") && config.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(config.Secret))
		_, _ = mac.Write([]byte(timestamp + "\n" + config.Secret))
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return err
		}
		query := parsed.Query()
		query.Set("timestamp", timestamp)
		query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		parsed.RawQuery = query.Encode()
		endpoint = parsed.String()
	}
	method := strings.ToUpper(config.Method)
	if method != http.MethodPost {
		method = http.MethodGet
	}
	var body io.Reader
	if method == http.MethodGet {
		if !strings.Contains(config.URL, "#") {
			parsed, err := url.Parse(endpoint)
			if err != nil {
				return err
			}
			query := parsed.Query()
			query.Set("title", event.Title)
			query.Set("message", event.Summary)
			parsed.RawQuery = query.Encode()
			endpoint = parsed.String()
		}
	} else {
		payload := config.Body
		if payload == "" {
			defaultPayload := map[string]any{"title": event.Title, "summary": event.Summary, "type": event.Type, "fields": event.Fields, "created_at": event.CreatedAt}
			if strings.EqualFold(config.Type, "FORM") {
				form := url.Values{"title": {event.Title}, "summary": {event.Summary}, "type": {event.Type}}
				payload = form.Encode()
			} else {
				encoded, _ := json.Marshal(defaultPayload)
				payload = string(encoded)
			}
		} else {
			payload = replaceTemplate(payload, replacements, strings.EqualFold(config.Type, "FORM"))
		}
		body = strings.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if method == http.MethodPost {
		if strings.EqualFold(config.Type, "FORM") {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if config.Headers != "" {
		var headers map[string]string
		if err = json.Unmarshal([]byte(config.Headers), &headers); err != nil {
			return fmt.Errorf("invalid webhook headers: %w", err)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook HTTP %d: %s", resp.StatusCode, string(responseBody))
	}
	return nil
}

func eventText(event domain.NotificationEvent) string {
	var builder strings.Builder
	builder.WriteString("[CDT Monitor] ")
	builder.WriteString(event.Title)
	builder.WriteByte('\n')
	builder.WriteString(event.Summary)
	for key, value := range event.Fields {
		builder.WriteByte('\n')
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(value)
	}
	return builder.String()
}

func replacements(event domain.NotificationEvent) map[string]string {
	traffic := event.Fields["当前流量"]
	traffic = strings.TrimSpace(strings.TrimSuffix(traffic, "GB"))
	threshold := event.Fields["设定阈值"]
	threshold = strings.TrimSpace(strings.TrimSuffix(threshold, "%"))
	instance := event.Fields["实例"]
	status := event.Fields["实例状态"]
	createdAt := event.CreatedAt.UTC().Format(time.RFC3339)
	return map[string]string{
		"#TITLE#":             event.Title,
		"#MSG#":               event.Summary,
		"#ACCOUNT#":           strconv.FormatInt(event.AccountID, 10),
		"#ACCOUNT_ID#":        strconv.FormatInt(event.AccountID, 10),
		"#TRAFFIC#":           traffic,
		"#TRAFFIC_GB#":        traffic,
		"#MAX_TRAFFIC#":       threshold,
		"#THRESHOLD_PERCENT#": threshold,
		"#INSTANCE#":          instance,
		"#STATUS#":            status,
		"#TYPE#":              event.Type,
		"#CREATED_AT#":        createdAt,
		"#TIME#":              createdAt,
	}
}

func replaceTemplate(input string, replacements map[string]string, urlEncode bool) string {
	for key, value := range replacements {
		if urlEncode {
			value = url.QueryEscape(value)
		} else {
			encoded, _ := json.Marshal(value)
			value = strings.Trim(string(encoded), "\"")
		}
		input = strings.ReplaceAll(input, key, value)
	}
	return input
}

// ReadDotResponse is kept private to avoid accepting unbounded SMTP responses.
func readDotResponse(reader *bufio.Reader) ([]byte, error) {
	var buffer bytes.Buffer
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		if string(line) == ".\r\n" {
			return buffer.Bytes(), nil
		}
		buffer.Write(line)
	}
}
