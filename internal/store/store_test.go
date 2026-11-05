package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/wang4386/CDT-Monitor/internal/domain"
	"github.com/wang4386/CDT-Monitor/internal/security"
	_ "modernc.org/sqlite"
)

func TestMigratesLegacySecretsAndPassword(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", dir+"/data.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE accounts (id INTEGER PRIMARY KEY AUTOINCREMENT, access_key_id TEXT, access_key_secret TEXT, region_id TEXT, instance_id TEXT, max_traffic REAL, schedule_enabled INTEGER DEFAULT 0, start_time TEXT, stop_time TEXT, traffic_used REAL DEFAULT 0, instance_status TEXT DEFAULT 'Unknown', updated_at INTEGER DEFAULT 0, last_keep_alive_at INTEGER DEFAULT 0);
INSERT INTO settings(key,value) VALUES('admin_password','legacy-password'),('notify_tg_token','legacy-token');
INSERT INTO accounts(access_key_id,access_key_secret,region_id,instance_id,max_traffic) VALUES('LTAIlegacy','legacy-secret','cn-hongkong','i-legacy',200);
`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var token, secret string
	if err = st.db.QueryRow(`SELECT value FROM settings WHERE key='notify_tg_token'`).Scan(&token); err != nil {
		t.Fatal(err)
	}
	if err = st.db.QueryRow(`SELECT access_key_secret FROM accounts WHERE id=1`).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if !security.IsEncrypted(token) || !security.IsEncrypted(secret) {
		t.Fatal("legacy secrets were not encrypted")
	}
	valid, err := st.VerifyAdminPassword(context.Background(), "legacy-password")
	if err != nil || !valid {
		t.Fatalf("legacy password failed: %v", err)
	}
	var hash string
	_ = st.db.QueryRow(`SELECT value FROM settings WHERE key='admin_password'`).Scan(&hash)
	if hash == "legacy-password" {
		t.Fatal("legacy password was not upgraded")
	}
}

func TestAccountIDsRemainStable(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	config := domain.Config{AdminPassword: "Strong-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 600, Timezone: "Asia/Shanghai", Accounts: []domain.Account{{AccessKeyID: "LTAItest", AccessKeySecret: "secret", RegionID: "cn-hongkong", InstanceID: "i-test", MaxTraffic: 200, SiteType: "china"}}}
	if err = st.Setup(ctx, config); err != nil {
		t.Fatal(err)
	}
	accounts, _ := st.ListAccounts(ctx)
	id := accounts[0].ID
	config.AdminPassword = ""
	config.Accounts[0].ID = id
	config.Accounts[0].AccessKeySecret = ""
	config.Accounts[0].Remark = "updated"
	if err = st.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	accounts, _ = st.ListAccounts(ctx)
	if accounts[0].ID != id || accounts[0].Remark != "updated" {
		t.Fatal("account ID was not stable")
	}
}

func TestRuntimePublicIPPersistsAndClearsWhenInstanceChanges(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	config := domain.Config{AdminPassword: "Strong-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 600, Timezone: "Asia/Shanghai", Accounts: []domain.Account{{AccessKeyID: "LTAItest", AccessKeySecret: "secret", RegionID: "cn-hongkong", InstanceID: "i-test", MaxTraffic: 200, SiteType: "china"}}}
	if err = st.Setup(ctx, config); err != nil {
		t.Fatal(err)
	}
	accounts, err := st.ListAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts=%v err=%v", accounts, err)
	}
	updated := time.Date(2026, time.October, 30, 10, 0, 0, 0, time.UTC)
	if err = st.UpdateRuntimeWithIPs(ctx, accounts[0].ID, 1.5, domain.StatusRunning, "203.0.113.7", "2001:db8::7", updated, updated); err != nil {
		t.Fatal(err)
	}
	config.AdminPassword = ""
	config.Accounts[0].ID = accounts[0].ID
	config.Accounts[0].AccessKeySecret = ""
	if err = st.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetAccount(ctx, accounts[0].ID)
	if err != nil || loaded.PublicIP != "203.0.113.7" || loaded.PublicIPv6 != "2001:db8::7" || loaded.StartCron != "" {
		t.Fatalf("preserved runtime=%+v err=%v", loaded, err)
	}
	config.Accounts[0].InstanceID = "i-new"
	if err = st.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	loaded, err = st.GetAccount(ctx, accounts[0].ID)
	if err != nil || loaded.PublicIP != "" || loaded.PublicIPv6 != "" || !loaded.PublicIPUpdatedAt.IsZero() {
		t.Fatalf("stale public IP was not cleared: %+v err=%v", loaded, err)
	}
}

func TestCronSettingsPersist(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	config := domain.Config{AdminPassword: "Strong-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 600, Timezone: "Asia/Shanghai", Accounts: []domain.Account{{AccessKeyID: "LTAItest", AccessKeySecret: "secret", RegionID: "cn-hongkong", InstanceID: "i-test", MaxTraffic: 200, SiteType: "china"}}}
	if err = st.Setup(ctx, config); err != nil {
		t.Fatal(err)
	}
	accounts, _ := st.ListAccounts(ctx)
	if err = st.UpdateAccountSettingsWithCron(ctx, accounts[0].ID, nil, "", false, "", "", nil, "00:00", "0 0 1 * *", "0 0 15 * *", true); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetAccount(ctx, accounts[0].ID)
	if err != nil || loaded.StartCron != "0 0 1 * *" || loaded.StopCron != "0 0 15 * *" || loaded.ScheduleMode != "advanced" {
		t.Fatalf("cron settings=%+v err=%v", loaded, err)
	}
	if err = st.UpdateAccountSettingsWithScheduleMode(ctx, accounts[0].ID, nil, "", true, "simple", "08:00", "23:30", nil, "00:00", loaded.StartCron, loaded.StopCron, true); err != nil {
		t.Fatal(err)
	}
	loaded, err = st.GetAccount(ctx, accounts[0].ID)
	if err != nil || loaded.ScheduleMode != "simple" || loaded.StartCron == "" {
		t.Fatalf("daily mode should preserve advanced expressions for later: account=%+v err=%v", loaded, err)
	}
}

func TestAPIIntervalMinimum(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	config := domain.Config{AdminPassword: "Strong-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 29, Timezone: "Asia/Shanghai"}
	if err = st.Setup(ctx, config); err == nil {
		t.Fatal("expected API interval below 30 seconds to be rejected")
	}

	config.APIInterval = 30
	if err = st.Setup(ctx, config); err != nil {
		t.Fatal(err)
	}
	if _, err = st.db.ExecContext(ctx, `UPDATE settings SET value='1' WHERE key='api_interval'`); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.APIInterval != minAPIIntervalSeconds {
		t.Fatalf("expected legacy interval to normalize to %d seconds, got %d", minAPIIntervalSeconds, loaded.APIInterval)
	}
}

func TestAPIKeyScopesAndRevocation(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	key, token, err := st.CreateAPIKey(ctx, "widget", []string{"widget:read"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := st.ValidateAPIKey(ctx, token)
	if err != nil || len(scopes) != 1 || scopes[0] != "widget:read" {
		t.Fatalf("scopes=%v err=%v", scopes, err)
	}
	if err = st.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = st.ValidateAPIKey(ctx, token); err == nil {
		t.Fatal("revoked key must fail")
	}
	keys, err := st.ListAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("revoked key should not be listed as active: %#v", keys)
	}
}

func TestListAPIKeysReturnsEmptyArrayWhenNoneExist(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	keys, err := st.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if keys == nil || len(keys) != 0 {
		t.Fatalf("expected a non-nil empty API key list, got %#v", keys)
	}
}

func TestListLogsReturnsEmptyArrayAfterClear(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	if err = st.AddLog(ctx, "audit", "test log"); err != nil {
		t.Fatal(err)
	}
	if err = st.ClearLogs(ctx, "action"); err != nil {
		t.Fatal(err)
	}
	entries, err := st.ListLogs(ctx, "action", 100)
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("expected a non-nil empty log list, got %#v", entries)
	}
}

func TestActionEventCanBeReleasedAfterFailure(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	fresh, err := st.RecordActionEvent(ctx, "schedule:1:day:start", 1, "schedule_start", "attempting", "")
	if err != nil || !fresh {
		t.Fatal(err)
	}
	fresh, _ = st.RecordActionEvent(ctx, "schedule:1:day:start", 1, "schedule_start", "attempting", "")
	if fresh {
		t.Fatal("duplicate event must be rejected")
	}
	if err = st.DeleteActionEvent(ctx, "schedule:1:day:start"); err != nil {
		t.Fatal(err)
	}
	fresh, err = st.RecordActionEvent(ctx, "schedule:1:day:start", 1, "schedule_start", "attempting", "")
	if err != nil || !fresh {
		t.Fatal("released event should be retryable")
	}
}

func TestMonitorJobKeepsMinuteDeduplicationAfterCompletion(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	job, err := st.EnqueueJob(ctx, "monitor_account", 1, `{}`, "monitor:1:202607191200", 3)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.CompleteJob(ctx, job.ID, "ok"); err != nil {
		t.Fatal(err)
	}
	duplicate, err := st.EnqueueJob(ctx, "monitor_account", 1, `{}`, "monitor:1:202607191200", 3)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != job.ID {
		t.Fatalf("duplicate created new job %s instead of %s", duplicate.ID, job.ID)
	}
}

func TestSessionExpiry(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	token, err := st.CreateSession(context.Background(), "127.0.0.1", "test", -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := st.ValidateSession(context.Background(), token)
	if err != nil || valid {
		t.Fatal("expired session must not validate")
	}
}

func TestUpdateAdminPasswordKeepsCurrentSessionOnly(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err = st.Setup(ctx, domain.Config{AdminPassword: "Original-Password-42!", TrafficThreshold: 95, ShutdownMode: "KeepCharging", ThresholdAction: "stop_and_notify", APIInterval: 600, Timezone: "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}
	current, err := st.CreateSession(ctx, "127.0.0.1", "current", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateSession(ctx, "127.0.0.2", "other", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateAdminPassword(ctx, "Replacement-Password-84!", current); err != nil {
		t.Fatal(err)
	}
	if valid, _ := st.VerifyAdminPassword(ctx, "Original-Password-42!"); valid {
		t.Fatal("old password must no longer validate")
	}
	if valid, _ := st.VerifyAdminPassword(ctx, "Replacement-Password-84!"); !valid {
		t.Fatal("new password must validate")
	}
	if valid, _ := st.ValidateSession(ctx, current); !valid {
		t.Fatal("current session must remain valid")
	}
	if valid, _ := st.ValidateSession(ctx, other); valid {
		t.Fatal("other sessions must be invalidated")
	}
}

func TestPasskeyCredentialRoundTrip(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	credential := webauthn.Credential{ID: []byte("credential-id"), PublicKey: []byte("public-key")}
	if err = st.SavePasskey(ctx, "workstation", credential); err != nil {
		t.Fatal(err)
	}
	credentials, err := st.LoadPasskeyCredentials(ctx)
	if err != nil || len(credentials) != 1 || string(credentials[0].ID) != "credential-id" {
		t.Fatalf("credentials=%#v err=%v", credentials, err)
	}
	items, err := st.ListPasskeys(ctx)
	if err != nil || len(items) != 1 || items[0].Name != "workstation" {
		t.Fatalf("passkeys=%#v err=%v", items, err)
	}
	if err = st.UpdatePasskeyCredential(ctx, credential); err != nil {
		t.Fatal(err)
	}
	items, err = st.ListPasskeys(ctx)
	if err != nil || items[0].LastUsedAt == nil {
		t.Fatal("passkey last-used timestamp was not updated")
	}
	if err = st.DeletePasskey(ctx, items[0].ID); err != nil {
		t.Fatal(err)
	}
	items, err = st.ListPasskeys(ctx)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("expected non-nil empty passkey list, got %#v", items)
	}
}

func TestInterruptedWorkRecoversOnOpen(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.db.Exec(`INSERT INTO jobs(id,type,status,available_at,locked_at,created_at,updated_at) VALUES('stale','monitor_account','running',unixepoch()-300,unixepoch()-300,unixepoch()-300,unixepoch()-300)`)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var status string
	if err = st.db.QueryRow(`SELECT status FROM jobs WHERE id='stale'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "queued" {
		t.Fatalf("status = %s", status)
	}
}

func TestLegacyTrafficTablesMigrateToStableAccountIDs(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", dir+"/data.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE accounts (id INTEGER PRIMARY KEY AUTOINCREMENT, access_key_id TEXT, access_key_secret TEXT, region_id TEXT, instance_id TEXT, max_traffic REAL, schedule_enabled INTEGER DEFAULT 0, start_time TEXT, stop_time TEXT, traffic_used REAL DEFAULT 0, instance_status TEXT DEFAULT 'Unknown', updated_at INTEGER DEFAULT 0, last_keep_alive_at INTEGER DEFAULT 0);
CREATE TABLE traffic_hourly (id INTEGER PRIMARY KEY AUTOINCREMENT, access_key_id TEXT, traffic REAL, recorded_at INTEGER);
CREATE TABLE traffic_daily (id INTEGER PRIMARY KEY AUTOINCREMENT, access_key_id TEXT, traffic REAL, recorded_at INTEGER);
INSERT INTO accounts(id,access_key_id) VALUES(7,'legacy-ak');
INSERT INTO traffic_hourly(access_key_id,traffic,recorded_at) VALUES('legacy-ak',12.5,100);
INSERT INTO traffic_daily(access_key_id,traffic,recorded_at) VALUES('legacy-ak',25,200);
`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var accountID int64
	if err = st.db.QueryRow(`SELECT account_id FROM traffic_hourly LIMIT 1`).Scan(&accountID); err != nil || accountID != 7 {
		t.Fatalf("hourly account_id=%d err=%v", accountID, err)
	}
	if err = st.db.QueryRow(`SELECT account_id FROM traffic_daily LIMIT 1`).Scan(&accountID); err != nil || accountID != 7 {
		t.Fatalf("daily account_id=%d err=%v", accountID, err)
	}
}

func TestCopyAccountReusesSecretUnderSameAK(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	trueVal := true
	falseVal := false

	config := domain.Config{
		AdminPassword:     "Strong-Password-42!",
		TrafficThreshold:  95,
		ShutdownMode:      "KeepCharging",
		ThresholdAction:   "stop_and_notify",
		APIInterval:       600,
		Timezone:          "Asia/Shanghai",
		EnableDailyReport: true,
		DailyReportTime:   "23:30",
		Accounts: []domain.Account{
			{
				AccessKeyID:     "LTAIshared",
				AccessKeySecret: "super-secret-key",
				RegionID:        "cn-hongkong",
				InstanceID:      "i-source",
				MaxTraffic:      200,
				SiteType:        "china",
				Remark:          "源实例",
				KeepAlive:       &trueVal,
				ShutdownMode:    "StopCharging",
				ScheduleEnabled: true,
				StartTime:       "08:00",
				StopTime:        "22:00",
				DailyReport:     &trueVal,
			},
		},
	}
	if err = st.Setup(ctx, config); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	accounts, err := st.ListAccounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("expected 1 account: %v", err)
	}
	source := accounts[0]
	if source.KeepAlive == nil || !*source.KeepAlive || source.ShutdownMode != "StopCharging" || source.DailyReport == nil || !*source.DailyReport {
		t.Fatalf("source account settings mismatch: %+v", source)
	}

	// Copy instance: id=0, AccessKeySecret="", same AK
	duplicate := domain.Account{
		ID:              0,
		AccessKeyID:     "LTAIshared",
		AccessKeySecret: "", // Empty secret! Must reuse from existing account
		RegionID:        "cn-shanghai",
		InstanceID:      "i-copied",
		MaxTraffic:      100,
		SiteType:        "china",
		Remark:          "源实例 (副本)",
		KeepAlive:       &falseVal,
		ShutdownMode:    "KeepCharging",
		ScheduleEnabled: false,
		DailyReport:     &falseVal,
	}

	config.Accounts = append(config.Accounts, duplicate)
	config.Accounts[0].AccessKeySecret = "" // Also empty when submitting full config
	if err = st.SaveConfig(ctx, config); err != nil {
		t.Fatalf("SaveConfig failed on copied account: %v", err)
	}

	updated, err := st.ListAccounts(ctx)
	if err != nil || len(updated) != 2 {
		t.Fatalf("expected 2 accounts: %v", err)
	}

	// Verify both accounts have valid secrets
	sec0, err0 := st.AccountSecret(ctx, updated[0].ID)
	sec1, err1 := st.AccountSecret(ctx, updated[1].ID)
	if err0 != nil || err1 != nil || sec0 != "super-secret-key" || sec1 != "super-secret-key" {
		t.Fatalf("secret reuse failed: sec0=%s, sec1=%s", sec0, sec1)
	}

	if updated[1].KeepAlive == nil || *updated[1].KeepAlive != false || updated[1].ShutdownMode != "KeepCharging" || updated[1].DailyReport == nil || *updated[1].DailyReport != false {
		t.Fatalf("copied account settings mismatch: %+v", updated[1])
	}
}

func TestUpdateAccountSettingsDirectly(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	config := domain.Config{
		AdminPassword:    "Strong-Password-42!",
		TrafficThreshold: 95,
		ShutdownMode:     "KeepCharging",
		ThresholdAction:  "stop_and_notify",
		APIInterval:      600,
		Timezone:         "Asia/Shanghai",
		Accounts: []domain.Account{
			{AccessKeyID: "LTAI1", AccessKeySecret: "secret", RegionID: "cn-hongkong", InstanceID: "i-1", MaxTraffic: 100},
		},
	}
	if err = st.Setup(ctx, config); err != nil {
		t.Fatal(err)
	}
	accounts, _ := st.ListAccounts(ctx)
	id := accounts[0].ID

	newKeepAlive := true
	newDailyReport := false
	if err = st.UpdateAccountSettings(ctx, id, &newKeepAlive, "StopCharging", true, "09:30", "21:30", &newDailyReport, "01:15"); err != nil {
		t.Fatalf("UpdateAccountSettings failed: %v", err)
	}

	loaded, err := st.GetAccount(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.KeepAlive == nil || !*loaded.KeepAlive {
		t.Fatal("KeepAlive was not updated")
	}
	if loaded.ShutdownMode != "StopCharging" {
		t.Fatalf("ShutdownMode was not updated: %s", loaded.ShutdownMode)
	}
	if !loaded.ScheduleEnabled || loaded.StartTime != "09:30" || loaded.StopTime != "21:30" {
		t.Fatalf("Schedule was not updated: %+v", loaded)
	}
	if loaded.DailyReport == nil || *loaded.DailyReport != false {
		t.Fatal("DailyReport was not updated")
	}
}

func TestTrafficSnapshotRoundtrip(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	if err = st.RecordTrafficSnapshot(ctx, 101, "2026-09-19", "start", 12.34, "08:00"); err != nil {
		t.Fatal(err)
	}
	if err = st.RecordTrafficSnapshot(ctx, 101, "2026-09-19", "stop", 15.67, "22:00"); err != nil {
		t.Fatal(err)
	}

	snap, err := st.GetTrafficSnapshot(ctx, 101, "2026-09-19")
	if err != nil {
		t.Fatal(err)
	}
	if snap.StartTraffic != 12.34 || snap.StopTraffic != 15.67 || snap.StartTime != "08:00" || snap.StopTime != "22:00" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestTraffic24HoursAgoAndAroundTime(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	now := time.Now().UTC()
	t24Ago := now.Add(-24 * time.Hour)
	t12Ago := now.Add(-12 * time.Hour)

	_ = st.AddTrafficStats(ctx, 201, 10.0, t24Ago)
	_ = st.AddTrafficStats(ctx, 201, 12.5, t12Ago)
	_ = st.AddTrafficStats(ctx, 201, 15.292, now)

	traffic24, ok := st.Traffic24HoursAgo(ctx, 201, now)
	if !ok || traffic24 != 10.0 {
		t.Fatalf("expected 10.0 from 24h ago, got %v (ok=%v)", traffic24, ok)
	}

	trafficAround, ok := st.TrafficAroundTime(ctx, 201, t12Ago)
	if !ok || trafficAround != 12.5 {
		t.Fatalf("expected 12.5 around 12h ago, got %v (ok=%v)", trafficAround, ok)
	}
}

func TestTrafficAroundTimeIgnoresSamplesOutsideWindow(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	start := time.Date(2026, 9, 24, 8, 0, 0, 0, loc)
	if err = st.AddTrafficStats(ctx, 202, 51.98, start.Add(-8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = st.AddTrafficStats(ctx, 202, 54.44, start.Add(16*time.Hour+10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if traffic, ok := st.TrafficAroundTime(ctx, 202, start); ok {
		t.Fatalf("sample outside the start window was accepted: %v", traffic)
	}
}

func TestTraffic24HoursAgoDoesNotUseCurrentDayDailySampleNearMidnight(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	location := time.UTC
	now := time.Date(2026, 9, 23, 23, 56, 0, 0, location)
	previousDayStart := time.Date(2026, 9, 22, 0, 0, 0, 0, location).Unix()
	currentDayStart := time.Date(2026, 9, 23, 0, 0, 0, 0, location).Unix()
	if _, err = st.db.ExecContext(ctx, `INSERT INTO traffic_daily(account_id,traffic,recorded_at) VALUES(?,?,?),(?,?,?)`, 202, 10.0, previousDayStart, 202, 15.0, currentDayStart); err != nil {
		t.Fatal(err)
	}

	traffic, ok := st.Traffic24HoursAgo(ctx, 202, now)
	if !ok || traffic != 10.0 {
		t.Fatalf("expected previous day's baseline 10.0, got %v (ok=%v)", traffic, ok)
	}
}
