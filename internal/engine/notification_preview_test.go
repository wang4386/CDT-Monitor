package engine

import (
	"testing"

	"github.com/wang4386/CDT-Monitor/internal/domain"
)

func TestApplyNotificationPreviewKeepsStoredSecrets(t *testing.T) {
	stored := domain.Config{Notifications: domain.NotificationConfig{
		Email:    domain.EmailConfig{Password: "stored-email-password", Host: "old.smtp.example"},
		Telegram: domain.TelegramConfig{Token: "stored-bot-token", ProxyPass: "stored-proxy-password", ChatID: "old"},
		Webhook:  domain.WebhookConfig{URL: "https://old.example", Headers: "{\"X-Test\":\"stored\"}", Secret: "stored-signing-secret"},
	}}

	email := applyNotificationPreview(stored, "email", domain.NotificationConfig{
		Email: domain.EmailConfig{Host: "new.smtp.example", To: "new@example.com"},
	})
	if email.Notifications.Email.Host != "new.smtp.example" || email.Notifications.Email.Password != "stored-email-password" {
		t.Fatalf("email preview did not use current form and stored password: %+v", email.Notifications.Email)
	}
	if email.Notifications.Telegram.ChatID != "old" {
		t.Fatal("email preview changed the Telegram configuration")
	}

	telegram := applyNotificationPreview(stored, "telegram", domain.NotificationConfig{
		Telegram: domain.TelegramConfig{ChatID: "new"},
	})
	if telegram.Notifications.Telegram.ChatID != "new" || telegram.Notifications.Telegram.Token != "stored-bot-token" || telegram.Notifications.Telegram.ProxyPass != "stored-proxy-password" {
		t.Fatalf("telegram preview did not preserve stored credentials: %+v", telegram.Notifications.Telegram)
	}

	webhook := applyNotificationPreview(stored, "webhook", domain.NotificationConfig{
		Webhook: domain.WebhookConfig{URL: "https://new.example", Method: "POST", Type: "JSON"},
	})
	if webhook.Notifications.Webhook.URL != "https://new.example" || webhook.Notifications.Webhook.Headers != "{\"X-Test\":\"stored\"}" || webhook.Notifications.Webhook.Secret != "stored-signing-secret" {
		t.Fatalf("webhook preview did not preserve stored credentials: %+v", webhook.Notifications.Webhook)
	}
	if stored.Notifications.Webhook.URL != "https://old.example" {
		t.Fatal("preview modified the original saved configuration")
	}
}

func TestApplyNotificationPreviewPrefersNewCredentials(t *testing.T) {
	stored := domain.Config{Notifications: domain.NotificationConfig{
		Email:    domain.EmailConfig{Password: "old"},
		Telegram: domain.TelegramConfig{Token: "old", ProxyPass: "old"},
		Webhook:  domain.WebhookConfig{Headers: "old", Secret: "old"},
	}}
	email := applyNotificationPreview(stored, "email", domain.NotificationConfig{
		Email: domain.EmailConfig{Password: "new"},
	})
	telegram := applyNotificationPreview(stored, "telegram", domain.NotificationConfig{
		Telegram: domain.TelegramConfig{Token: "new", ProxyPass: "new"},
	})
	webhook := applyNotificationPreview(stored, "webhook", domain.NotificationConfig{
		Webhook: domain.WebhookConfig{Headers: "new", Secret: "new"},
	})
	if email.Notifications.Email.Password != "new" || telegram.Notifications.Telegram.Token != "new" || telegram.Notifications.Telegram.ProxyPass != "new" || webhook.Notifications.Webhook.Headers != "new" || webhook.Notifications.Webhook.Secret != "new" {
		t.Fatal("preview did not prefer user-entered credentials")
	}
}

func TestDailyReportNotificationPreviewUsesAllCurrentChannels(t *testing.T) {
	saved := domain.Config{Notifications: domain.NotificationConfig{
		Email: domain.EmailConfig{Host: "old.mail.example", Password: "old-mail-pass", Enabled: true},
		Telegram: domain.TelegramConfig{ChatID: "old-chat", Token: "saved-bot-token", Enabled: true},
		Webhook: domain.WebhookConfig{URL: "https://old.example", Headers: `{"X-Saved":"value"}`, Secret: "saved-secret", Enabled: true},
	}}
	draft := domain.NotificationConfig{
		Email: domain.EmailConfig{Host: "new.mail.example", Enabled: true},
		Telegram: domain.TelegramConfig{ChatID: "new-chat", Enabled: true},
		Webhook: domain.WebhookConfig{URL: "https://new.example", Enabled: true},
	}
	for _, channel := range []string{"email", "telegram", "webhook"} {
		saved = applyNotificationPreview(saved, channel, draft)
	}
	if saved.Notifications.Email.Host != "new.mail.example" || saved.Notifications.Email.Password != "old-mail-pass" {
		t.Fatal("email current settings / saved password were not combined")
	}
	if saved.Notifications.Telegram.ChatID != "new-chat" || saved.Notifications.Telegram.Token != "saved-bot-token" {
		t.Fatal("telegram current settings / saved token were not combined")
	}
	if saved.Notifications.Webhook.URL != "https://new.example" || saved.Notifications.Webhook.Headers != `{"X-Saved":"value"}` || saved.Notifications.Webhook.Secret != "saved-secret" {
		t.Fatal("webhook current settings / saved secrets were not combined")
	}
}
