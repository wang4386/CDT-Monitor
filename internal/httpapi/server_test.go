package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/wang4386/CDT-Monitor/internal/domain"
	"github.com/wang4386/CDT-Monitor/internal/engine"
	"github.com/wang4386/CDT-Monitor/internal/store"
)

func TestSecurityHeadersAllowFaviconEndpoint(t *testing.T) {
	server := &Server{}
	handler := server.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://monitor.example.com/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	csp := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "img-src 'self' data: https://a.favicon.im") {
		t.Fatalf("favicon endpoint missing from CSP: %s", csp)
	}
	if !strings.Contains(csp, "connect-src 'self' https://api.github.com") {
		t.Fatalf("GitHub API endpoint missing from CSP: %s", csp)
	}
}

func TestNormalizeClockTimeUses24HourValues(t *testing.T) {
	for input, want := range map[string]string{"00:34": "00:34", "00：34": "00:34", "24:00": "00:00"} {
		got, err := normalizeClockTime(input, true)
		if err != nil || got != want {
			t.Errorf("normalizeClockTime(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := normalizeClockTime("12:34 AM", true); err == nil {
		t.Fatal("expected non-HH:MM time input to be rejected")
	}
}

func TestBeginPasskeyLoginIncludesRegisteredCredentialIDs(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	credentialID := []byte("desktop-bitwarden-credential")
	if err = st.SavePasskey(t.Context(), "PC Bitwarden", webauthn.Credential{ID: credentialID, PublicKey: []byte("test-key")}); err != nil {
		t.Fatal(err)
	}

	server := &Server{store: st, limits: make(map[string]*rateWindow), passkeys: make(map[string]passkeySession)}
	request := httptest.NewRequest(http.MethodPost, "http://monitor.example.com/api/v1/auth/passkeys/begin", strings.NewReader(`{}`))
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	server.beginPasskeyLogin(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		SessionID string `json:"session_id"`
		PublicKey struct {
			PublicKey struct {
				AllowCredentials []struct {
					ID string `json:"id"`
				} `json:"allowCredentials"`
			} `json:"publicKey"`
		} `json:"public_key"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.PublicKey.PublicKey.AllowCredentials) != 1 || payload.PublicKey.PublicKey.AllowCredentials[0].ID != base64.RawURLEncoding.EncodeToString(credentialID) {
		t.Fatalf("allowCredentials = %#v", payload.PublicKey.PublicKey.AllowCredentials)
	}
	ceremony, ok := server.passkeys[payload.SessionID]
	if !ok || string(ceremony.session.UserID) != adminWebAuthnID {
		t.Fatalf("login session does not identify the admin user: %#v", ceremony.session.UserID)
	}
}

func TestBeginPasskeyLoginRejectsEmptyCredentialList(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server := &Server{store: st, limits: make(map[string]*rateWindow), passkeys: make(map[string]passkeySession)}
	request := httptest.NewRequest(http.MethodPost, "http://monitor.example.com/api/v1/auth/passkeys/begin", strings.NewReader(`{}`))
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	server.beginPasskeyLogin(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestRefreshAllEnqueuesEveryConfiguredAccount(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	config := domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 600, Timezone: "Asia/Shanghai",
		Accounts: []domain.Account{
			{AccessKeyID: "LTAItest-one", AccessKeySecret: "secret-one", RegionID: "cn-hongkong", InstanceID: "i-one", MaxTraffic: 200, SiteType: "china"},
			{AccessKeyID: "LTAItest-two", AccessKeySecret: "secret-two", RegionID: "ap-southeast-1", InstanceID: "i-two", MaxTraffic: 300, SiteType: "international"},
		},
	}
	if err = st.Setup(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st, nil, nil, slog.Default(), 1)
	server := &Server{store: st, engine: eng}
	request := httptest.NewRequest(http.MethodPost, "http://monitor.example.com/api/v1/accounts/refresh", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	server.refreshAll(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Jobs []domain.Job `json:"jobs"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Jobs) != 2 {
		t.Fatalf("jobs = %#v", payload.Jobs)
	}
	if payload.Jobs[0].Type != engine.JobRefreshAccount || payload.Jobs[1].Type != engine.JobRefreshAccount || payload.Jobs[0].AccountID == payload.Jobs[1].AccountID {
		t.Fatalf("unexpected refresh jobs: %#v", payload.Jobs)
	}
}

func TestStatusIncludesAssignedIPv6AndOmitsMissingIPv6(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	config := domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 600, Timezone: "Asia/Shanghai",
		Accounts: []domain.Account{
			{AccessKeyID: "LTAI-ipv6-one", AccessKeySecret: "secret-one", RegionID: "cn-hongkong", InstanceID: "i-one", MaxTraffic: 200, SiteType: "china"},
			{AccessKeyID: "LTAI-ipv6-two", AccessKeySecret: "secret-two", RegionID: "ap-southeast-1", InstanceID: "i-two", MaxTraffic: 200, SiteType: "international"},
		},
	}
	if err = st.Setup(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	accounts, err := st.ListAccounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = st.UpdateRuntimeWithIPs(t.Context(), accounts[0].ID, 1, domain.StatusRunning, "203.0.113.1", "2001:db8::1", now, now); err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateRuntimeWithIPs(t.Context(), accounts[1].ID, 1, domain.StatusRunning, "203.0.113.2", "", now, now); err != nil {
		t.Fatal(err)
	}

	server := &Server{store: st, engine: engine.New(st, nil, nil, slog.Default(), 1)}
	response := httptest.NewRecorder()
	server.status(response, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Accounts []map[string]json.RawMessage `json:"accounts"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 2 {
		t.Fatalf("accounts = %#v", payload.Accounts)
	}
	var ipv6 string
	if err = json.Unmarshal(payload.Accounts[0]["public_ipv6"], &ipv6); err != nil || ipv6 != "2001:db8::1" {
		t.Fatalf("first public_ipv6 = %q, err = %v", ipv6, err)
	}
	if _, exists := payload.Accounts[1]["public_ipv6"]; exists {
		t.Fatalf("public_ipv6 should be omitted when not assigned: %#v", payload.Accounts[1])
	}
}

func TestUpdateAccountSettingsEndpoint(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	config := domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 600, Timezone: "Asia/Shanghai",
		Accounts: []domain.Account{
			{AccessKeyID: "LTAI-settings-test", AccessKeySecret: "secret-one", RegionID: "cn-hongkong", InstanceID: "i-one", MaxTraffic: 200, SiteType: "china"},
		},
	}
	if err = st.Setup(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st, nil, nil, slog.Default(), 1)
	server := &Server{store: st, engine: eng}

	accounts, _ := st.ListAccounts(t.Context())
	id := accounts[0].ID

	body := `{"keep_alive":true,"shutdown_mode":"StopCharging","schedule_enabled":true,"schedule_mode":"advanced","start_time":"09:00","stop_time":"00:34","daily_report":false,"daily_report_time":"02:45","start_cron":"0 9 * * 1","stop_cron":"30 0 * * 1"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/accounts/1/settings", strings.NewReader(body))
	req.SetPathValue("id", "1")
	resp := httptest.NewRecorder()

	server.updateAccountSettings(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	updated, err := st.GetAccount(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if updated.KeepAlive == nil || !*updated.KeepAlive {
		t.Fatal("expected keep_alive=true")
	}
	if updated.ShutdownMode != "StopCharging" {
		t.Fatalf("expected shutdown_mode=StopCharging, got %s", updated.ShutdownMode)
	}
	if !updated.ScheduleEnabled || updated.ScheduleMode != "advanced" || updated.StartTime != "09:00" || updated.StopTime != "00:34" || updated.StartCron != "0 9 * * 1" || updated.StopCron != "30 0 * * 1" {
		t.Fatalf("unexpected schedule: %+v", updated)
	}
	if updated.DailyReport == nil || *updated.DailyReport != false {
		t.Fatal("expected daily_report=false")
	}

	// Test resetting back to default (null / default)
	bodyReset := `{"keep_alive":null,"shutdown_mode":"default","schedule_enabled":false,"schedule_mode":"simple","start_time":"08:00","stop_time":"22:00","daily_report":null,"start_cron":"","stop_cron":""}`
	reqReset := httptest.NewRequest(http.MethodPatch, "/api/v1/accounts/1/settings", strings.NewReader(bodyReset))
	reqReset.SetPathValue("id", "1")
	respReset := httptest.NewRecorder()

	server.updateAccountSettings(respReset, reqReset)
	if respReset.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", respReset.Code, respReset.Body.String())
	}

	resetAccount, err := st.GetAccount(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if resetAccount.KeepAlive != nil {
		t.Fatalf("expected keep_alive=nil, got %v", *resetAccount.KeepAlive)
	}
	if resetAccount.ShutdownMode != "" {
		t.Fatalf("expected shutdown_mode='', got %s", resetAccount.ShutdownMode)
	}
	if resetAccount.DailyReport != nil {
		t.Fatalf("expected daily_report=nil, got %v", *resetAccount.DailyReport)
	}
}

func TestTriggerDailyReportEndpoint(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	config := domain.Config{
		AdminPassword: "Strong-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 600, Timezone: "Asia/Shanghai",
		EnableDailyReport: true,
	}
	if err = st.Setup(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st, nil, nil, slog.Default(), 1)
	server := &Server{store: st, engine: eng}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/daily-report", strings.NewReader(`{}`))
	resp := httptest.NewRecorder()

	server.triggerDailyReport(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var job domain.Job
	if err = json.Unmarshal(resp.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Type != engine.JobDailyReport {
		t.Fatalf("expected job type %s, got %s", engine.JobDailyReport, job.Type)
	}
}
