package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wang4386/CDT-Monitor/internal/aliyun"
	"github.com/wang4386/CDT-Monitor/internal/domain"
	"github.com/wang4386/CDT-Monitor/internal/engine"
	"github.com/wang4386/CDT-Monitor/internal/notify"
	"github.com/wang4386/CDT-Monitor/internal/store"
)

func TestNotificationPreviewUsesUnsavedWebhookWithoutPersisting(t *testing.T) {
	oldRequests, previewRequests := 0, 0
	oldEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldRequests++
		w.WriteHeader(http.StatusOK)
	}))
	defer oldEndpoint.Close()
	previewEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		previewRequests++
		var payload struct {
			Text string "json:\"text\""
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Text != "unsaved" {
			t.Errorf("unexpected preview webhook body: %+v, err=%v", payload, err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer previewEndpoint.Close()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	config := domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95,
		ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify",
		APIInterval: 600, Timezone: "Asia/Shanghai",
		Notifications: domain.NotificationConfig{Webhook: domain.WebhookConfig{
			Enabled: true, URL: oldEndpoint.URL, Method: "POST", Type: "JSON", Body: "{\"text\":\"saved\"}",
		}},
	}
	if err := st.Setup(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: st, engine: engine.New(st, nil, notify.New(), slog.Default(), 1)}

	preview := domain.NotificationConfig{Webhook: domain.WebhookConfig{
		Enabled: true, URL: previewEndpoint.URL, Method: "POST", Type: "JSON", Body: "{\"text\":\"unsaved\"}",
	}}
	body, err := json.Marshal(map[string]any{"notifications": preview})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/test/webhook", strings.NewReader(string(body)))
	request.SetPathValue("channel", "webhook")
	response := httptest.NewRecorder()
	server.testNotification(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
	}
	if previewRequests != 1 || oldRequests != 0 {
		t.Fatalf("expected only the unsaved endpoint to receive a request: preview=%d saved=%d", previewRequests, oldRequests)
	}
	persisted, err := st.GetConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Notifications.Webhook.URL != oldEndpoint.URL || persisted.Notifications.Webhook.Body != "{\"text\":\"saved\"}" {
		t.Fatal("preview unexpectedly persisted the unsaved webhook configuration")
	}
}

func TestNotificationPreviewRejectsInvalidJSON(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/test/webhook", strings.NewReader("{\"notifications\":{\"webhook\":{"))
	request.SetPathValue("channel", "webhook")
	response := httptest.NewRecorder()
	server.testNotification(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", response.Code)
	}
}

func TestNotificationTestLegacyEmptyBodyStillEnqueues(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	config := domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95,
		ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify",
		APIInterval: 600, Timezone: "Asia/Shanghai",
	}
	if err := st.Setup(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: st, engine: engine.New(st, nil, notify.New(), slog.Default(), 1)}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/test/webhook", nil)
	request.SetPathValue("channel", "webhook")
	response := httptest.NewRecorder()
	server.testNotification(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d, want 202; body=%s", response.Code, response.Body.String())
	}
}

func TestDailyReportPreviewUsesUnsavedWebhookAndPreservesStoredConfig(t *testing.T) {
	oldRequests, previewRequests := 0, 0
	oldEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldRequests++
		w.WriteHeader(http.StatusOK)
	}))
	defer oldEndpoint.Close()
	previewEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		previewRequests++
		if r.Header.Get("X-Stored") != "keep" {
			t.Errorf("saved sensitive webhook headers not reused: %q", r.Header.Get("X-Stored"))
		}
		var event struct {
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil || event.Type != "daily_report" {
			t.Errorf("unexpected daily report event: %+v, err=%v", event, err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer previewEndpoint.Close()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	config := domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95,
		ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify",
		APIInterval: 600, Timezone: "Asia/Shanghai",
		Accounts: []domain.Account{{
			AccessKeyID: "LTAI-daily-preview", AccessKeySecret: "secret-test", RegionID: "cn-hongkong", InstanceID: "i-daily-preview", MaxTraffic: 200, SiteType: "china",
		}},
		Notifications: domain.NotificationConfig{Webhook: domain.WebhookConfig{
			Enabled: true, URL: oldEndpoint.URL, Method: "POST", Type: "JSON", Headers: `{"X-Stored":"keep"}`,
		}},
	}
	if err := st.Setup(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	preview := domain.NotificationConfig{Webhook: domain.WebhookConfig{
		Enabled: true, URL: previewEndpoint.URL, Method: "POST", Type: "JSON",
	}}
	body, err := json.Marshal(map[string]any{"notifications": preview})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/daily-report", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	server := &Server{store: st, engine: engine.New(st, dailyPreviewProvider{}, notify.New(), slog.Default(), 1)}
	server.triggerDailyReport(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
	}
	if previewRequests != 1 || oldRequests != 0 {
		t.Fatalf("daily report went to wrong webhook: preview=%d saved=%d", previewRequests, oldRequests)
	}
	persisted, err := st.GetConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Notifications.Webhook.URL != oldEndpoint.URL {
		t.Fatal("daily report preview persisted temporary settings")
	}
}

type dailyPreviewProvider struct{}

func (dailyPreviewProvider) GetTraffic(context.Context, domain.Account, string) (float64, error) {
	return 1.25, nil
}
func (dailyPreviewProvider) GetInstanceStatus(context.Context, domain.Account, string) (string, error) {
	return domain.StatusRunning, nil
}
func (dailyPreviewProvider) GetInstanceDetails(context.Context, domain.Account, string) (aliyun.InstanceDetails, error) {
	return aliyun.InstanceDetails{Status: domain.StatusRunning}, nil
}
func (dailyPreviewProvider) ControlInstance(context.Context, domain.Account, string, string, string) error {
	return nil
}
func (dailyPreviewProvider) GetAccountBalance(context.Context, domain.Account, string) (aliyun.BillingBalance, error) {
	return aliyun.BillingBalance{}, nil
}
func (dailyPreviewProvider) GetInstanceBill(context.Context, domain.Account, string, string) (aliyun.BillingBill, error) {
	return aliyun.BillingBill{}, nil
}

func TestDailyReportLegacyEmptyBodyStillEnqueues(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Setup(t.Context(), domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95,
		ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify",
		APIInterval: 600, Timezone: "Asia/Shanghai",
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: st, engine: engine.New(st, nil, notify.New(), slog.Default(), 1)}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/daily-report", strings.NewReader("{}"))
	response := httptest.NewRecorder()
	server.triggerDailyReport(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("legacy request status=%d, want 202; body=%s", response.Code, response.Body.String())
	}
}
