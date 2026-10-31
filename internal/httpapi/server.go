package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/wang4386/CDT-Monitor/internal/domain"
	"github.com/wang4386/CDT-Monitor/internal/engine"
	"github.com/wang4386/CDT-Monitor/internal/scheduler"
	"github.com/wang4386/CDT-Monitor/internal/security"
	"github.com/wang4386/CDT-Monitor/internal/store"
)

type principal struct {
	admin  bool
	apiKey bool
	scopes map[string]bool
}

type contextKey string

const principalKey contextKey = "principal"

type Server struct {
	store    *store.Store
	engine   *engine.Engine
	logger   *slog.Logger
	assets   fs.FS
	handler  http.Handler
	mu       sync.Mutex
	limits   map[string]*rateWindow
	passkeys map[string]passkeySession
	build    BuildInfo
}

type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

type passkeySession struct {
	kind    string
	name    string
	session webauthn.SessionData
	expires time.Time
}

const adminWebAuthnID = "cdt-monitor-admin-v1"

type rateWindow struct {
	start time.Time
	count int
}

func New(st *store.Store, eng *engine.Engine, assets fs.FS, logger *slog.Logger, build ...BuildInfo) *Server {
	info := BuildInfo{Version: "dev", Commit: "unknown", BuiltAt: "unknown"}
	if len(build) > 0 {
		info = build[0]
	}
	s := &Server{store: st, engine: eng, assets: assets, logger: logger, limits: make(map[string]*rateWindow), passkeys: make(map[string]passkeySession), build: info}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/v1/system/init-status", s.initStatus)
	mux.Handle("GET /api/v1/system/info", s.require("admin", http.HandlerFunc(s.systemInfo)))
	mux.HandleFunc("POST /api/v1/setup", s.setup)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/passkeys/begin", s.beginPasskeyLogin)
	mux.HandleFunc("POST /api/v1/auth/passkeys/complete", s.completePasskeyLogin)
	mux.Handle("POST /api/v1/auth/logout", s.require("admin", http.HandlerFunc(s.logout)))
	mux.Handle("PUT /api/v1/admin/password", s.require("admin", http.HandlerFunc(s.updateAdminPassword)))
	mux.Handle("GET /api/v1/admin/passkeys", s.require("admin", http.HandlerFunc(s.listPasskeys)))
	mux.Handle("DELETE /api/v1/admin/passkeys/{id}", s.require("admin", http.HandlerFunc(s.deletePasskey)))
	mux.Handle("POST /api/v1/admin/passkeys/register/begin", s.require("admin", http.HandlerFunc(s.beginPasskeyRegistration)))
	mux.Handle("POST /api/v1/admin/passkeys/register/complete", s.require("admin", http.HandlerFunc(s.completePasskeyRegistration)))
	mux.Handle("GET /api/v1/status", s.require("widget:read", http.HandlerFunc(s.status)))
	mux.Handle("GET /api/v1/widget/summary", s.require("widget:read", http.HandlerFunc(s.widgetSummary)))
	mux.Handle("GET /api/v1/config", s.require("admin", http.HandlerFunc(s.getConfig)))
	mux.Handle("PUT /api/v1/config", s.require("admin", http.HandlerFunc(s.saveConfig)))
	mux.Handle("GET /api/v1/accounts/{id}/history", s.require("widget:read", http.HandlerFunc(s.history)))
	mux.Handle("POST /api/v1/accounts/refresh", s.require("instance:control", http.HandlerFunc(s.refreshAll)))
	mux.Handle("POST /api/v1/accounts/{id}/refresh", s.require("instance:control", http.HandlerFunc(s.refresh)))
	mux.Handle("POST /api/v1/accounts/{id}/actions/{action}", s.require("instance:control", http.HandlerFunc(s.control)))
	mux.Handle("PATCH /api/v1/accounts/{id}/settings", s.require("admin", http.HandlerFunc(s.updateAccountSettings)))
	mux.Handle("POST /api/v1/schedules/preview", s.require("admin", http.HandlerFunc(s.previewSchedule)))
	mux.Handle("POST /api/v1/notifications/daily-report", s.require("admin", http.HandlerFunc(s.triggerDailyReport)))
	mux.Handle("GET /api/v1/jobs/{id}", s.require("widget:read", http.HandlerFunc(s.job)))
	mux.Handle("GET /api/v1/logs", s.require("admin", http.HandlerFunc(s.logs)))
	mux.Handle("DELETE /api/v1/logs", s.require("admin", http.HandlerFunc(s.clearLogs)))
	mux.Handle("POST /api/v1/notifications/test/{channel}", s.require("admin", http.HandlerFunc(s.testNotification)))
	mux.Handle("GET /api/v1/api-keys", s.require("admin", http.HandlerFunc(s.apiKeys)))
	mux.Handle("POST /api/v1/api-keys", s.require("admin", http.HandlerFunc(s.createAPIKey)))
	mux.Handle("DELETE /api/v1/api-keys/{id}", s.require("admin", http.HandlerFunc(s.revokeAPIKey)))
	mux.HandleFunc("GET /monitor.php", s.legacyMonitor)
	mux.Handle("/", s.staticHandler())
	s.handler = s.securityHeaders(s.recover(mux))
	return s
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ready(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_not_ready", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *Server) initStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.store.IsInitialized(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "init_status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"initialized": initialized})
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	response := map[string]any{
		"version":     s.build.Version,
		"commit":      s.build.Commit,
		"built_at":    s.build.BuiltAt,
		"repository":  "https://github.com/wang4386/CDT-Monitor",
		"release_url": "https://github.com/wang4386/CDT-Monitor/releases",
	}
	if r.URL.Query().Get("check") == "1" {
		latest, err := latestRelease(r.Context(), s.build.Version)
		if err != nil {
			response["check_error"] = "暂时无法检查 GitHub Release"
		} else {
			response["latest_version"] = latest
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.allowRate("setup:"+clientIP(r), 5, time.Minute) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁")
		return
	}
	var config domain.Config
	if err := decodeJSON(r, &config); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	applyConfigDefaults(&config)
	if err := validateAccountCrons(config.Accounts); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cron", err.Error())
		return
	}
	if err := s.store.Setup(r.Context(), config); err != nil {
		writeError(w, http.StatusBadRequest, "setup_failed", err.Error())
		return
	}
	_ = s.store.AddLog(r.Context(), "audit", "系统初始化完成 [IP: "+clientIP(r)+"]")
	token, err := s.store.CreateSession(r.Context(), clientIP(r), r.UserAgent(), 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", err.Error())
		return
	}
	csrf := newCSRFToken()
	setAuthCookies(w, r, token, csrf)
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "csrf_token": csrf})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.allowRate("login:"+ip, 8, 15*time.Minute) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "登录尝试过多，请稍后再试")
		return
	}
	failures, err := s.store.RecentLoginFailures(r.Context(), ip, time.Now().Add(-15*time.Minute))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login_failed", err.Error())
		return
	}
	if failures >= 5 {
		writeError(w, http.StatusTooManyRequests, "login_locked", "登录已临时锁定 15 分钟")
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if err = decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	valid, err := s.store.VerifyAdminPassword(r.Context(), request.Password)
	if err != nil || !valid {
		_ = s.store.RecordLoginFailure(r.Context(), ip)
		_ = s.store.AddLog(r.Context(), "warning", "管理员登录失败 [IP: "+ip+"]")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "密码错误")
		return
	}
	_ = s.store.ClearLoginFailures(r.Context(), ip)
	token, err := s.store.CreateSession(r.Context(), ip, r.UserAgent(), 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", err.Error())
		return
	}
	csrf := newCSRFToken()
	setAuthCookies(w, r, token, csrf)
	_ = s.store.AddLog(r.Context(), "audit", "管理员登录成功 [IP: "+ip+"]")
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "csrf_token": csrf})
}

func (s *Server) updateAdminPassword(w http.ResponseWriter, r *http.Request) {
	var request struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	valid, err := s.store.VerifyAdminPassword(r.Context(), request.CurrentPassword)
	if err != nil || !valid {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "当前密码错误")
		return
	}
	if len(request.NewPassword) < 10 {
		writeError(w, http.StatusBadRequest, "invalid_password", "新密码至少需要 10 个字符")
		return
	}
	currentToken := ""
	if cookie, cookieErr := r.Cookie("cdt_session"); cookieErr == nil {
		currentToken = cookie.Value
	}
	if err := s.store.UpdateAdminPassword(r.Context(), request.NewPassword, currentToken); err != nil {
		writeError(w, http.StatusInternalServerError, "password_update_failed", "管理员密码更新失败")
		return
	}
	_ = s.store.AddLog(r.Context(), "audit", "管理员密码已更新")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListPasskeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkeys_failed", "Passkey 列表加载失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": items})
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	if err := s.store.DeletePasskey(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "Passkey 删除失败")
		return
	}
	_ = s.store.AddLog(r.Context(), "audit", "删除管理员 Passkey")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	if !requestSecure(r) {
		writeError(w, http.StatusBadRequest, "https_required", "Passkey 只能在 HTTPS 安全上下文中创建")
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	credentials, err := s.store.LoadPasskeyCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "Passkey 数据加载失败")
		return
	}
	creation, session, err := s.webAuthn(r).BeginRegistration(&adminWebAuthnUser{credentials: credentials})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", err.Error())
		return
	}
	id, err := security.NewToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "无法创建 Passkey 挑战")
		return
	}
	s.savePasskeySession(id, passkeySession{kind: "registration", name: strings.TrimSpace(request.Name), session: *session, expires: time.Now().Add(5 * time.Minute)})
	writeJSON(w, http.StatusOK, map[string]any{"session_id": id, "public_key": creation})
}

func (s *Server) completePasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	if !requestSecure(r) {
		writeError(w, http.StatusBadRequest, "https_required", "Passkey 只能在 HTTPS 安全上下文中创建")
		return
	}
	ceremony, ok := s.takePasskeySession(r.URL.Query().Get("session_id"), "registration")
	if !ok {
		writeError(w, http.StatusBadRequest, "passkey_session_expired", "Passkey 挑战已过期，请重新开始")
		return
	}
	credentials, err := s.store.LoadPasskeyCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "Passkey 数据加载失败")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponse(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "passkey_invalid", "Passkey 响应无效")
		return
	}
	credential, err := s.webAuthn(r).CreateCredential(&adminWebAuthnUser{credentials: credentials}, ceremony.session, parsed)
	if err != nil {
		writeError(w, http.StatusBadRequest, "passkey_invalid", "Passkey 验证失败")
		return
	}
	if err = s.store.SavePasskey(r.Context(), ceremony.name, *credential); err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "Passkey 保存失败")
		return
	}
	_ = s.store.AddLog(r.Context(), "audit", "创建管理员 Passkey")
	writeJSON(w, http.StatusCreated, map[string]any{"success": true})
}

func (s *Server) beginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !requestSecure(r) {
		writeError(w, http.StatusBadRequest, "https_required", "Passkey 登录只能在 HTTPS 安全上下文中使用")
		return
	}
	credentials, err := s.store.LoadPasskeyCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "Passkey 数据加载失败")
		return
	}
	user := &adminWebAuthnUser{credentials: credentials}
	if len(credentials) == 0 {
		writeError(w, http.StatusNotFound, "passkey_not_configured", "尚未创建管理员 Passkey")
		return
	}
	assertion, session, err := s.webAuthn(r).BeginLogin(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", err.Error())
		return
	}
	id, err := security.NewToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "无法创建 Passkey 挑战")
		return
	}
	s.savePasskeySession(id, passkeySession{kind: "login", session: *session, expires: time.Now().Add(5 * time.Minute)})
	writeJSON(w, http.StatusOK, map[string]any{"session_id": id, "public_key": assertion})
}

func (s *Server) completePasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !requestSecure(r) {
		writeError(w, http.StatusBadRequest, "https_required", "Passkey 登录只能在 HTTPS 安全上下文中使用")
		return
	}
	ceremony, ok := s.takePasskeySession(r.URL.Query().Get("session_id"), "login")
	if !ok {
		writeError(w, http.StatusBadRequest, "passkey_session_expired", "Passkey 挑战已过期，请重新开始")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponse(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "passkey_invalid", "Passkey 响应无效")
		return
	}
	credentials, err := s.store.LoadPasskeyCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "Passkey 数据加载失败")
		return
	}
	user := &adminWebAuthnUser{credentials: credentials}
	credential, err := s.webAuthn(r).ValidateLogin(user, ceremony.session, parsed)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "passkey_invalid", "Passkey 验证失败")
		return
	}
	if err = s.store.UpdatePasskeyCredential(r.Context(), *credential); err != nil {
		writeError(w, http.StatusInternalServerError, "passkey_failed", "Passkey 状态保存失败")
		return
	}
	token, err := s.store.CreateSession(r.Context(), clientIP(r), r.UserAgent(), 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", err.Error())
		return
	}
	csrf := newCSRFToken()
	setAuthCookies(w, r, token, csrf)
	_ = s.store.AddLog(r.Context(), "audit", "管理员使用 Passkey 登录成功 [IP: "+clientIP(r)+"]")
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "csrf_token": csrf})
}

type adminWebAuthnUser struct {
	credentials []webauthn.Credential
}

func (u *adminWebAuthnUser) WebAuthnID() []byte                         { return []byte(adminWebAuthnID) }
func (u *adminWebAuthnUser) WebAuthnName() string                       { return "admin" }
func (u *adminWebAuthnUser) WebAuthnDisplayName() string                { return "CDT Monitor 管理员" }
func (u *adminWebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (s *Server) webAuthn(r *http.Request) *webauthn.WebAuthn {
	origin := requestOrigin(r)
	parsed, err := url.Parse(origin)
	host := r.Host
	if err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	return &webauthn.WebAuthn{Config: &webauthn.Config{
		RPID:          host,
		RPDisplayName: "CDT Monitor",
		RPOrigins:     []string{origin},
	}}
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if requestSecure(r) {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if strings.Contains(host, ",") {
		host = strings.TrimSpace(strings.Split(host, ",")[0])
	}
	return scheme + "://" + host
}

func (s *Server) savePasskeySession(id string, session passkeySession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, item := range s.passkeys {
		if item.expires.Before(now) {
			delete(s.passkeys, key)
		}
	}
	s.passkeys[id] = session
}

func (s *Server) takePasskeySession(id, kind string) (passkeySession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.passkeys[id]
	if !ok || session.kind != kind || session.expires.Before(time.Now()) {
		delete(s.passkeys, id)
		return passkeySession{}, false
	}
	delete(s.passkeys, id)
	return session, true
}

func latestRelease(ctx context.Context, version string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/wang4386/CDT-Monitor/releases/latest", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if version == "" {
		version = "dev"
	}
	request.Header.Set("User-Agent", "CDT-Monitor/"+version)
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("github HTTP %d", response.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", errors.New("latest release has no tag")
	}
	return payload.TagName, nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("cdt_session"); err == nil {
		_ = s.store.DeleteSession(r.Context(), cookie.Value)
	}
	clearAuthCookies(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	accounts, lastRun, err := s.engine.Summary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "status_failed", err.Error())
		return
	}
	etag := fmt.Sprintf(`W/"%d-%d"`, lastRun.Unix(), len(accounts))
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=15")
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts, "system_last_run": lastRun})
}

func (s *Server) widgetSummary(w http.ResponseWriter, r *http.Request) {
	accounts, lastRun, err := s.engine.Summary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "status_failed", err.Error())
		return
	}
	type compact struct {
		ID         int64     `json:"id"`
		Name       string    `json:"name"`
		Status     string    `json:"status"`
		Used       float64   `json:"used"`
		Total      float64   `json:"total"`
		Percentage float64   `json:"percentage"`
		Updated    time.Time `json:"updated_at"`
	}
	items := make([]compact, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, compact{ID: account.ID, Name: firstNonEmpty(account.Remark, account.Account), Status: account.InstanceStatus, Used: account.FlowUsed, Total: account.FlowTotal, Percentage: account.Percentage, Updated: account.LastUpdated})
	}
	w.Header().Set("Cache-Control", "private, max-age=30")
	writeJSON(w, http.StatusOK, map[string]any{"accounts": items, "updated_at": lastRun})
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.store.GetConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", err.Error())
		return
	}
	scrubConfig(&config)
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) saveConfig(w http.ResponseWriter, r *http.Request) {
	var config domain.Config
	if err := decodeJSON(r, &config); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	applyConfigDefaults(&config)
	if err := validateAccountCrons(config.Accounts); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cron", err.Error())
		return
	}
	for index := range config.Accounts {
		account := &config.Accounts[index]
		if account.ScheduleMode != "simple" && account.ScheduleMode != "advanced" {
			account.ScheduleMode = "simple"
			if account.StartCron != "" || account.StopCron != "" {
				account.ScheduleMode = "advanced"
			}
		}
		var err error
		account.StartTime, err = normalizeClockTime(account.StartTime, account.ScheduleEnabled)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_schedule_time", "开机时间必须使用 24 小时制 HH:MM 格式")
			return
		}
		account.StopTime, err = normalizeClockTime(account.StopTime, account.ScheduleEnabled)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_schedule_time", "关机时间必须使用 24 小时制 HH:MM 格式")
			return
		}
	}
	if err := s.store.SaveConfig(r.Context(), config); err != nil {
		writeError(w, http.StatusBadRequest, "config_failed", err.Error())
		return
	}
	_ = s.store.AddLog(r.Context(), "audit", "管理员更新系统配置")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	history, err := s.store.History(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "history_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	job, err := s.engine.Enqueue(r.Context(), engine.JobRefreshAccount, id, `{}`, engine.JobUniqueKey(engine.JobRefreshAccount, id, time.Now().UTC().Format("200601021504")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) refreshAll(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.engine.EnqueueRefreshAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobs": jobs})
}

func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	action := strings.ToLower(r.PathValue("action"))
	if action != "start" && action != "stop" {
		writeError(w, http.StatusBadRequest, "invalid_action", "action must be start or stop")
		return
	}
	job, err := s.engine.Enqueue(r.Context(), engine.JobControlInstance, id, engine.ParseControlPayload(action, "手动"), engine.JobUniqueKey(engine.JobControlInstance, id, action))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) job(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "job_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.ListLogs(r.Context(), r.URL.Query().Get("tab"), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": entries})
}

func (s *Server) clearLogs(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearLogs(r.Context(), r.URL.Query().Get("tab")); err != nil {
		writeError(w, http.StatusInternalServerError, "logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	if channel != "email" && channel != "telegram" && channel != "webhook" {
		writeError(w, http.StatusBadRequest, "invalid_channel", "invalid notification channel")
		return
	}
	job, err := s.engine.Enqueue(r.Context(), engine.JobTestNotify, 0, engine.ParseNotifyPayload(channel), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) apiKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.ListAPIKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "api_keys_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	for _, scope := range request.Scopes {
		if scope != "widget:read" && scope != "instance:control" && scope != "cron:run" {
			writeError(w, http.StatusBadRequest, "invalid_scope", "invalid API key scope")
			return
		}
	}
	key, token, err := s.store.CreateAPIKey(r.Context(), request.Name, request.Scopes, request.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "api_key_failed", err.Error())
		return
	}
	_ = s.store.AddLog(r.Context(), "audit", "创建 API Key: "+request.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"key": key, "token": token})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	if err := s.store.RevokeAPIKey(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "api_key_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) legacyMonitor(w http.ResponseWriter, r *http.Request) {
	scopes, err := s.store.ValidateAPIKey(r.Context(), r.URL.Query().Get("key"))
	if err != nil || !contains(scopes, "cron:run") {
		writeError(w, http.StatusUnauthorized, "invalid_key", "需要具有 cron:run 权限的 API Key")
		return
	}
	if err = s.engine.RunOnce(r.Context()); err != nil {
		if errors.Is(err, engine.ErrMonitorBusy) {
			writeError(w, http.StatusConflict, "monitor_busy", "监控任务正在由其他进程执行")
			return
		}
		writeError(w, http.StatusConflict, "monitor_busy", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}

func (s *Server) require(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请登录或提供有效 API Key")
			return
		}
		if !p.admin && !p.scopes[scope] {
			writeError(w, http.StatusForbidden, "forbidden", "API Key 权限不足")
			return
		}
		if p.admin && r.Method != http.MethodGet && r.Method != http.MethodHead && !validCSRF(r) {
			writeError(w, http.StatusForbidden, "csrf_failed", "CSRF 校验失败")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

func (s *Server) authenticate(r *http.Request) (principal, error) {
	token := bearerToken(r)
	if token != "" {
		scopes, err := s.store.ValidateAPIKey(r.Context(), token)
		if err != nil {
			return principal{}, err
		}
		p := principal{apiKey: true, scopes: make(map[string]bool)}
		for _, scope := range scopes {
			p.scopes[scope] = true
		}
		return p, nil
	}
	cookie, err := r.Cookie("cdt_session")
	if err != nil {
		return principal{}, err
	}
	valid, err := s.store.ValidateSession(r.Context(), cookie.Value)
	if err != nil || !valid {
		return principal{}, errors.New("invalid session")
	}
	return principal{admin: true, scopes: map[string]bool{"admin": true, "widget:read": true, "instance:control": true, "cron:run": true}}, nil
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func setAuthCookies(w http.ResponseWriter, r *http.Request, token, csrf string) {
	secure := requestSecure(r)
	http.SetCookie(w, &http.Cookie{Name: "cdt_session", Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	http.SetCookie(w, &http.Cookie{Name: "cdt_csrf", Value: csrf, Path: "/", HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
}

func clearAuthCookies(w http.ResponseWriter, r *http.Request) {
	secure := requestSecure(r)
	for _, name := range []string{"cdt_session", "cdt_csrf"} {
		http.SetCookie(w, &http.Cookie{Name: name, Path: "/", MaxAge: -1, HttpOnly: name == "cdt_session", Secure: secure, SameSite: http.SameSiteStrictMode})
	}
}

func validCSRF(r *http.Request) bool {
	cookie, err := r.Cookie("cdt_csrf")
	return err == nil && cookie.Value != "" && r.Header.Get("X-CDT-CSRF") == cookie.Value
}

func requestSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func newCSRFToken() string {
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (s *Server) staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not_found", "接口不存在")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(s.assets, path); err != nil {
			r.URL.Path = "/"
		}
		if strings.Contains(path, ".") && path != "index.html" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https://a.favicon.im; connect-src 'self' https://api.github.com; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "panic", recovered, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowRate(key string, max int, window time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	entry := s.limits[key]
	if entry == nil || now.Sub(entry.start) >= window {
		s.limits[key] = &rateWindow{start: now, count: 1}
		return true
	}
	if entry.count >= max {
		return false
	}
	entry.count++
	return true
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func pathInt64(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid_id", "无效 ID")
		return 0, false
	}
	return id, true
}

func applyConfigDefaults(config *domain.Config) {
	if config.TrafficThreshold == 0 {
		config.TrafficThreshold = 95
	}
	if config.ShutdownMode == "" {
		config.ShutdownMode = "KeepCharging"
	}
	if config.ThresholdAction == "" {
		config.ThresholdAction = "stop_and_notify"
	}
	if config.APIInterval == 0 {
		config.APIInterval = 600
	}
	if config.Timezone == "" {
		config.Timezone = "Asia/Shanghai"
	}
	if config.DailyReportTime == "" {
		config.DailyReportTime = "22:00"
	}
	if config.Notifications.Email.Port == 0 {
		config.Notifications.Email.Port = 465
	}
	if config.Notifications.Email.Security == "" {
		config.Notifications.Email.Security = "ssl"
	}
	if config.Notifications.Telegram.ProxyType == "" {
		config.Notifications.Telegram.ProxyType = "none"
	}
	if config.Notifications.Webhook.Method == "" {
		config.Notifications.Webhook.Method = "GET"
	}
	if config.Notifications.Webhook.Type == "" {
		config.Notifications.Webhook.Type = "JSON"
	}
	if config.Notifications.Webhook.Provider == "" {
		config.Notifications.Webhook.Provider = "generic"
	}
}

func validateAccountCrons(accounts []domain.Account) error {
	for index := range accounts {
		accounts[index].StartCron = strings.TrimSpace(accounts[index].StartCron)
		accounts[index].StopCron = strings.TrimSpace(accounts[index].StopCron)
		if err := validateCronPair(accounts[index].StartCron, accounts[index].StopCron); err != nil {
			name := accounts[index].Remark
			if name == "" {
				name = fmt.Sprintf("实例 %d", index+1)
			}
			return fmt.Errorf("%s：%w", name, err)
		}
	}
	return nil
}

func validateCronPair(startCron, stopCron string) error {
	for label, expression := range map[string]string{"开机": startCron, "关机": stopCron} {
		if expression == "" {
			continue
		}
		if _, err := scheduler.Parse(expression); err != nil {
			return fmt.Errorf("%s Cron 表达式无效：%v", label, err)
		}
	}
	return nil
}

func scrubConfig(config *domain.Config) {
	config.AdminPassword = ""
	config.Notifications.Email.Password = ""
	config.Notifications.Telegram.Token = ""
	config.Notifications.Telegram.ProxyPass = ""
	config.Notifications.Webhook.Headers = ""
	config.Notifications.Webhook.Secret = ""
	for index := range config.Accounts {
		config.Accounts[index].AccessKeySecret = ""
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "CDT"
}

type NullableBool struct {
	Present bool
	Value   *bool
}

func (n *NullableBool) UnmarshalJSON(data []byte) error {
	n.Present = true
	s := strings.TrimSpace(string(data))
	if s == "null" || s == `"default"` {
		n.Value = nil
		return nil
	}
	var b bool
	if err := json.Unmarshal(data, &b); err != nil {
		return err
	}
	n.Value = &b
	return nil
}

type accountSettingsInput struct {
	KeepAlive       NullableBool `json:"keep_alive"`
	ShutdownMode    *string      `json:"shutdown_mode"`
	ScheduleEnabled *bool        `json:"schedule_enabled"`
	ScheduleMode    *string      `json:"schedule_mode"`
	StartTime       *string      `json:"start_time"`
	StopTime        *string      `json:"stop_time"`
	DailyReport     NullableBool `json:"daily_report"`
	DailyReportTime *string      `json:"daily_report_time"`
	StartCron       *string      `json:"start_cron"`
	StopCron        *string      `json:"stop_cron"`
}

func (s *Server) updateAccountSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	account, err := s.store.GetAccount(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "实例不存在")
		return
	}
	var input accountSettingsInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	keepAlive := account.KeepAlive
	if input.KeepAlive.Present {
		keepAlive = input.KeepAlive.Value
	}
	shutdownMode := account.ShutdownMode
	if input.ShutdownMode != nil {
		mode := *input.ShutdownMode
		if mode != "" && mode != "default" && mode != "KeepCharging" && mode != "StopCharging" {
			writeError(w, http.StatusBadRequest, "invalid_request", "无效的停机模式")
			return
		}
		if mode == "default" {
			mode = ""
		}
		shutdownMode = mode
	}
	scheduleEnabled := account.ScheduleEnabled
	if input.ScheduleEnabled != nil {
		scheduleEnabled = *input.ScheduleEnabled
	}
	mode := account.ScheduleMode
	if input.ScheduleMode != nil {
		mode = strings.TrimSpace(*input.ScheduleMode)
	}
	if mode == "" {
		mode = "simple"
		if account.StartCron != "" || account.StopCron != "" {
			mode = "advanced"
		}
	}
	if mode != "simple" && mode != "advanced" {
		writeError(w, http.StatusBadRequest, "invalid_schedule_mode", "定时任务模式必须为 simple 或 advanced")
		return
	}
	startTime := account.StartTime
	if input.StartTime != nil {
		startTime = *input.StartTime
	}
	stopTime := account.StopTime
	if input.StopTime != nil {
		stopTime = *input.StopTime
	}
	startTime, err = normalizeClockTime(startTime, scheduleEnabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_schedule_time", "开机时间必须使用 24 小时制 HH:MM 格式")
		return
	}
	stopTime, err = normalizeClockTime(stopTime, scheduleEnabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_schedule_time", "关机时间必须使用 24 小时制 HH:MM 格式")
		return
	}
	dailyReport := account.DailyReport
	if input.DailyReport.Present {
		dailyReport = input.DailyReport.Value
	}
	dailyReportTime := account.DailyReportTime
	if input.DailyReportTime != nil {
		dailyReportTime = *input.DailyReportTime
	}
	dailyReportTime, err = normalizeClockTime(dailyReportTime, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_daily_report_time", "日报时间必须使用 24 小时制 HH:MM 格式")
		return
	}
	startCron, stopCron := account.StartCron, account.StopCron
	if input.StartCron != nil {
		startCron = strings.TrimSpace(*input.StartCron)
	}
	if input.StopCron != nil {
		stopCron = strings.TrimSpace(*input.StopCron)
	}
	if err := validateCronPair(startCron, stopCron); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cron", err.Error())
		return
	}
	if err := s.store.UpdateAccountSettingsWithScheduleMode(r.Context(), id, keepAlive, shutdownMode, scheduleEnabled, mode, startTime, stopTime, dailyReport, dailyReportTime, startCron, stopCron, true); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	_ = s.store.AddLog(r.Context(), "audit", fmt.Sprintf("管理员更新实例 [%s] 配置", account.Remark))
	updated, err := s.store.GetAccount(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_failed", err.Error())
		return
	}
	updated.AccessKeySecret = ""
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) previewSchedule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Expression string `json:"expression"`
		Timezone   string `json:"timezone"`
		Count      int    `json:"count"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	input.Expression = strings.TrimSpace(input.Expression)
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	if input.Count == 0 {
		input.Count = 5
	}
	occurrences, err := scheduler.NextOccurrences(input.Expression, input.Timezone, time.Now(), input.Count)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cron", "Cron 表达式无效："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"expression": input.Expression, "timezone": input.Timezone, "occurrences": occurrences})
}

func normalizeClockTime(value string, required bool) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "：", ":"))
	if value == "" && !required {
		return "", nil
	}
	if value == "24:00" {
		return "00:00", nil
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return "", err
	}
	return parsed.Format("15:04"), nil
}

func (s *Server) triggerDailyReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID int64 `json:"account_id"`
	}
	_ = decodeJSON(r, &body)
	job, err := s.engine.EnqueueDailyReportForAccount(r.Context(), true, body.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
