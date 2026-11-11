package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wang4386/CDT-Monitor/internal/aliyun"
	"github.com/wang4386/CDT-Monitor/internal/domain"
	"github.com/wang4386/CDT-Monitor/internal/notify"
	"github.com/wang4386/CDT-Monitor/internal/scheduler"
	"github.com/wang4386/CDT-Monitor/internal/security"
	"github.com/wang4386/CDT-Monitor/internal/store"
)

const (
	JobMonitorAccount  = "monitor_account"
	JobRefreshAccount  = "refresh_account"
	JobControlInstance = "control_instance"
	JobTestNotify      = "test_notification"
	JobDailyReport     = "daily_report"
)

func resolveKeepAlive(account domain.Account, config domain.Config) bool {
	if account.KeepAlive != nil {
		return *account.KeepAlive
	}
	return config.KeepAlive
}

func resolveShutdownMode(account domain.Account, config domain.Config) string {
	if account.ShutdownMode == "KeepCharging" || account.ShutdownMode == "StopCharging" {
		return account.ShutdownMode
	}
	if config.ShutdownMode != "" {
		return config.ShutdownMode
	}
	return "KeepCharging"
}

type Engine struct {
	store        *store.Store
	provider     aliyun.Provider
	notify       *notify.Service
	logger       *slog.Logger
	owner        string
	wake         chan struct{}
	workers      int
	started      sync.Once
	accountLocks sync.Map
}

var ErrMonitorBusy = errors.New("monitor scheduler lease is held by another process")

func New(st *store.Store, provider aliyun.Provider, notifier *notify.Service, logger *slog.Logger, workers int) *Engine {
	if workers < 1 {
		workers = 4
	}
	owner, _ := security.NewToken(12)
	return &Engine{store: st, provider: provider, notify: notifier, logger: logger, owner: owner, wake: make(chan struct{}, 1), workers: workers}
}

func (e *Engine) Start(ctx context.Context) {
	e.started.Do(func() {
		go e.scheduler(ctx)
		for index := 0; index < e.workers; index++ {
			go e.worker(ctx, index)
		}
		go e.notificationWorker(ctx)
	})
}

func (e *Engine) Enqueue(ctx context.Context, jobType string, accountID int64, payload, uniqueKey string) (domain.Job, error) {
	maxAttempts := 3
	if jobType == JobDailyReport {
		// A transient traffic API outage at shutdown must not exhaust the
		// report job in only a few seconds.
		maxAttempts = 12
	}
	job, err := e.store.EnqueueJob(ctx, jobType, accountID, payload, uniqueKey, maxAttempts)
	if err == nil {
		e.signal()
	}
	return job, err
}

func (e *Engine) EnqueueRefreshAll(ctx context.Context) ([]domain.Job, error) {
	accounts, err := e.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	minute := time.Now().UTC().Format("200601021504")
	jobs := make([]domain.Job, 0, len(accounts))
	for _, account := range accounts {
		job, enqueueErr := e.Enqueue(ctx, JobRefreshAccount, account.ID, `{}`, JobUniqueKey(JobRefreshAccount, account.ID, minute))
		if enqueueErr != nil {
			return jobs, enqueueErr
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (e *Engine) RunOnce(ctx context.Context) error {
	acquired, err := e.store.AcquireLease(ctx, "monitor", e.owner, 75*time.Second)
	if err != nil {
		return err
	}
	if !acquired {
		return ErrMonitorBusy
	}
	e.checkDailyReport(ctx, time.Now())
	return e.enqueueMonitorCycle(ctx, time.Now())
}

func (e *Engine) scheduler(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	_ = e.RunOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := e.RunOnce(ctx); err != nil && !errors.Is(err, ErrMonitorBusy) {
				e.logger.Warn("scheduler cycle skipped", "error", err)
			}
			if now.Minute()%30 == 0 && now.Second() < 15 {
				_ = e.store.Prune(ctx)
			}
		}
	}
}

func (e *Engine) enqueueMonitorCycle(ctx context.Context, now time.Time) error {
	accounts, err := e.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	minute := now.UTC().Format("200601021504")
	for _, account := range accounts {
		uniqueKey := fmt.Sprintf("monitor:%d:%s", account.ID, minute)
		if _, err = e.Enqueue(ctx, JobMonitorAccount, account.ID, `{}`, uniqueKey); err != nil {
			return err
		}
	}
	return e.store.SetLastMonitorRun(ctx, now.UTC())
}

func (e *Engine) worker(ctx context.Context, index int) {
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.wake:
		case <-ticker.C:
		}
		for {
			job, err := e.store.ClaimJob(ctx)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				e.logger.Error("claim job", "worker", index, "error", err)
				break
			}
			result, runErr := e.runJob(ctx, job)
			if runErr != nil {
				e.logger.Warn("job failed", "job_id", job.ID, "type", job.Type, "error", runErr)
				_ = e.store.FailJob(ctx, job, runErr)
				continue
			}
			_ = e.store.CompleteJob(ctx, job.ID, result)
		}
	}
}

func (e *Engine) runJob(ctx context.Context, job domain.Job) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	switch job.Type {
	case JobMonitorAccount:
		return e.processAccount(ctx, job.AccountID, false)
	case JobRefreshAccount:
		return e.processAccount(ctx, job.AccountID, true)
	case JobControlInstance:
		var payload struct {
			Action string `json:"action"`
			Source string `json:"source"`
		}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return "", err
		}
		return e.control(ctx, job.AccountID, payload.Action, payload.Source)
	case JobTestNotify:
		var payload struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return "", err
		}
		return "notification sent", e.SendTestNotification(ctx, payload.Channel, nil)
	case JobDailyReport:
		var payload struct {
			Force      bool    `json:"force"`
			AccountID  int64   `json:"account_id"`
			AccountIDs []int64 `json:"account_ids"`
			ReportDate string  `json:"report_date"`
		}
		_ = json.Unmarshal([]byte(job.Payload), &payload)
		if len(payload.AccountIDs) > 0 {
			return e.generateAndSendDailyReportForAccounts(ctx, payload.AccountIDs, payload.ReportDate, job.ID)
		}
		return e.generateAndSendDailyReport(ctx, payload.Force, payload.AccountID)
	default:
		return "", fmt.Errorf("unknown job type %q", job.Type)
	}
}

// SendTestNotification uses optional unsaved notification settings without persisting them.
func (e *Engine) SendTestNotification(ctx context.Context, channel string, preview *domain.NotificationConfig) error {
	config, err := e.store.GetConfig(ctx)
	if err != nil {
		return err
	}
	if preview != nil {
		config = applyNotificationPreview(config, channel, *preview)
	}
	event := newEvent("test", "通知通道测试", "CDT Monitor 的通知配置工作正常。", 0, map[string]string{"发送时间": time.Now().Format("2006-01-02 15:04:05")})
	return e.notify.Send(ctx, channel, event, config)
}

// SendDailyReportPreview builds the normal report but uses the current unsaved
// notification settings for test delivery only.
func (e *Engine) SendDailyReportPreview(ctx context.Context, accountID int64, notifications domain.NotificationConfig) (string, error) {
	return e.generateAndSendDailyReportSelected(ctx, true, accountID, nil, "", "", &notifications)
}

// Empty secret fields mean "keep the stored credential", consistent with SaveConfig.
func applyNotificationPreview(current domain.Config, channel string, preview domain.NotificationConfig) domain.Config {
	switch channel {
	case "email":
		if preview.Email.Password == "" {
			preview.Email.Password = current.Notifications.Email.Password
		}
		current.Notifications.Email = preview.Email
	case "telegram":
		if preview.Telegram.Token == "" {
			preview.Telegram.Token = current.Notifications.Telegram.Token
		}
		if preview.Telegram.ProxyPass == "" {
			preview.Telegram.ProxyPass = current.Notifications.Telegram.ProxyPass
		}
		current.Notifications.Telegram = preview.Telegram
	case "webhook":
		if preview.Webhook.Headers == "" {
			preview.Webhook.Headers = current.Notifications.Webhook.Headers
		}
		if preview.Webhook.Secret == "" {
			preview.Webhook.Secret = current.Notifications.Webhook.Secret
		}
		current.Notifications.Webhook = preview.Webhook
	}
	return current
}

func (e *Engine) processAccount(ctx context.Context, accountID int64, force bool) (string, error) {
	lock := e.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	config, err := e.store.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	account, err := e.store.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	secret, err := e.store.AccountSecret(ctx, accountID)
	if err != nil {
		return "", err
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	now := time.Now().In(location)

	actions := make([]string, 0, 2)
	statusChangedBySchedule := false
	mode := account.ScheduleMode
	if mode == "" {
		mode = "simple"
		if account.StartCron != "" || account.StopCron != "" {
			mode = "advanced"
		}
	}
	dailyStartDue := account.ScheduleEnabled && mode == "simple" && dueWithin(now, account.StartTime, 10*time.Minute)
	dailyStopDue := account.ScheduleEnabled && mode == "simple" && dueWithin(now, account.StopTime, 10*time.Minute)
	cronStartAt, cronStartDue := scheduler.LastOccurrenceWithin(account.StartCron, now, 10*time.Minute)
	cronStopAt, cronStopDue := scheduler.LastOccurrenceWithin(account.StopCron, now, 10*time.Minute)
	cronStartDue = account.ScheduleEnabled && mode == "advanced" && cronStartDue
	cronStopDue = account.ScheduleEnabled && mode == "advanced" && cronStopDue
	scheduledAction, cronSchedule := "", false
	var cronOccurrence time.Time
	var cronExpression string
	if dailyStopDue {
		scheduledAction = "stop"
	} else if cronStopDue {
		scheduledAction, cronSchedule, cronOccurrence, cronExpression = "stop", true, cronStopAt, account.StopCron
	} else if dailyStartDue {
		scheduledAction = "start"
	} else if cronStartDue {
		scheduledAction, cronSchedule, cronOccurrence, cronExpression = "start", true, cronStartAt, account.StartCron
	}
	if scheduledAction != "" {
		var changed bool
		var runErr error
		if cronSchedule {
			changed, runErr = e.executeCronScheduledAction(ctx, config, account, secret, scheduledAction, cronExpression, cronOccurrence)
		} else {
			changed, runErr = e.executeScheduledAction(ctx, config, account, secret, scheduledAction, now)
		}
		if runErr != nil {
			return "", runErr
		}
		if changed {
			actionLabel := "scheduled_" + scheduledAction
			if cronSchedule {
				actionLabel = "cron_" + scheduledAction
			}
			actions = append(actions, actionLabel)
			if scheduledAction == "start" {
				account.InstanceStatus = domain.StatusStarting
			} else {
				account.InstanceStatus = domain.StatusStopping
			}
			statusChangedBySchedule = true
		}
	}

	interval := time.Duration(config.APIInterval) * time.Second
	if transient(account.InstanceStatus) {
		interval = time.Minute
	}
	due := force || account.UpdatedAt.IsZero() || time.Since(account.UpdatedAt) >= interval || now.Minute() == 0 || statusChangedBySchedule
	traffic, status := account.TrafficUsed, account.InstanceStatus
	if due {
		var trafficErr, statusErr, detailsErr error
		var details aliyun.InstanceDetails
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			traffic, trafficErr = e.provider.GetTraffic(ctx, account, secret)
		}()
		go func() {
			defer wait.Done()
			if account.InstanceID == "" {
				status, statusErr = e.provider.GetInstanceStatus(ctx, account, secret)
				return
			}
			details, detailsErr = e.provider.GetInstanceDetails(ctx, account, secret)
			if detailsErr == nil {
				status = details.Status
			}
		}()
		wait.Wait()
		if details.IPv6Error != nil {
			_ = e.store.AddLog(ctx, "warning", fmt.Sprintf("IPv6 补充查询失败 [%s]（需 ecs:DescribeNetworkInterfaces 权限）: %v", masked(account.AccessKeyID), details.IPv6Error))
			// Keep the last address on lookup failure; a successful empty result clears it.
			details.PublicIPv6 = account.PublicIPv6
		}
		if trafficErr != nil {
			traffic = account.TrafficUsed
			_ = e.store.AddLog(ctx, "error", fmt.Sprintf("流量查询失败 [%s]: %v", masked(account.AccessKeyID), trafficErr))
		}
		if statusErr != nil || detailsErr != nil || status == "" {
			status = account.InstanceStatus
			queryErr := statusErr
			if detailsErr != nil {
				queryErr = detailsErr
			}
			_ = e.store.AddLog(ctx, "error", fmt.Sprintf("实例详情查询失败 [%s]: %v", masked(account.AccessKeyID), queryErr))
		}
		updatedAt := time.Now().UTC()
		if trafficErr != nil {
			updatedAt = account.UpdatedAt
		}
		scheduledStart := slices.Contains(actions, "scheduled_start") || slices.Contains(actions, "cron_start")
		scheduledStop := slices.Contains(actions, "scheduled_stop") || slices.Contains(actions, "cron_stop")
		if statusChangedBySchedule {
			if scheduledStart {
				status = domain.StatusStarting
			} else if scheduledStop {
				status = domain.StatusStopping
			}
		}
		if detailsErr == nil && statusErr == nil && account.InstanceID != "" {
			ipUpdatedAt := time.Now().UTC()
			if err = e.store.UpdateRuntimeWithIPs(ctx, account.ID, traffic, status, details.PublicIP, details.PublicIPv6, ipUpdatedAt, updatedAt); err != nil {
				return "", err
			}
			account.PublicIP, account.PublicIPv6, account.PublicIPUpdatedAt = details.PublicIP, details.PublicIPv6, ipUpdatedAt
		} else if err = e.store.UpdateRuntime(ctx, account.ID, traffic, status, updatedAt); err != nil {
			return "", err
		}
		if trafficErr == nil {
			_ = e.store.AddTrafficStats(ctx, account.ID, traffic, now)
		}
		if statusChangedBySchedule {
			if slices.Contains(actions, "scheduled_start") && trafficErr == nil {
				_ = e.store.RecordTrafficSnapshot(ctx, account.ID, scheduleCycleDate(now, account.StartTime, account.StopTime), "start", traffic, now.Format("15:04"))
			}
			if slices.Contains(actions, "scheduled_stop") {
				if trafficErr == nil {
					_ = e.store.RecordTrafficSnapshot(ctx, account.ID, scheduleCycleDate(now, account.StartTime, account.StopTime), "stop", traffic, now.Format("15:04"))
				}
				if config.EnableDailyReport && (account.DailyReport == nil || *account.DailyReport) {
					reportKey := dailyReportKey(account, scheduleCycleDate(now, account.StartTime, account.StopTime))
					freshReport, _ := e.store.RecordActionEvent(ctx, reportKey, account.ID, "daily_report", "attempting", "")
					if freshReport {
						payload, _ := json.Marshal(map[string]any{"force": false, "account_id": account.ID})
						if _, err = e.Enqueue(ctx, JobDailyReport, account.ID, string(payload), reportKey); err != nil {
							_ = e.store.DeleteActionEvent(ctx, reportKey)
						}
					}
				}
			}
		}
	}

	percentage := usagePercent(traffic, account.MaxTraffic)
	overThreshold := percentage >= float64(config.TrafficThreshold)
	thresholdKey := fmt.Sprintf("threshold:%d:active", account.ID)
	if !overThreshold {
		_ = e.store.DeleteActionEvent(ctx, thresholdKey)
	}
	if overThreshold && due {
		key := thresholdKey
		recorded, recordErr := e.store.RecordActionEvent(ctx, key, account.ID, "threshold", "detected", fmt.Sprintf("%.2f%%", percentage))
		if recordErr != nil {
			return "", recordErr
		}
		if recorded {
			if config.ThresholdAction == "stop_and_notify" && status != domain.StatusStopped && status != domain.StatusStopping {
				effectiveShutdownMode := resolveShutdownMode(account, config)
				if err = e.provider.ControlInstance(ctx, account, secret, "stop", effectiveShutdownMode); err != nil {
					_ = e.store.DeleteActionEvent(ctx, key)
					return "", err
				}
				status = domain.StatusStopping
				_ = e.store.UpdateRuntime(ctx, account.ID, traffic, status, time.Now().UTC())
				actions = append(actions, "threshold_stop")
			}
			event := newEvent("threshold", "流量阈值告警", fmt.Sprintf("账号 %s 的流量使用率达到 %.2f%%。", masked(account.AccessKeyID), percentage), account.ID, map[string]string{
				"当前流量": fmt.Sprintf("%.2f GB", traffic), "设定阈值": fmt.Sprintf("%d%%", config.TrafficThreshold), "实例状态": status,
			})
			_ = e.store.AddOutbox(ctx, event, notify.EnabledChannels(config))
			_ = e.store.AddLog(ctx, "warning", event.Summary)
		}
	}

	effectiveKeepAlive := resolveKeepAlive(account, config)
	if effectiveKeepAlive && !overThreshold && !statusChangedBySchedule && status == domain.StatusStopped && (!usesSimpleSchedule(account) || inTimeRange(now.Format("15:04"), account.StartTime, account.StopTime)) {
		key := fmt.Sprintf("keepalive:%d:%s", account.ID, now.Format("200601021504"))
		fresh, recordErr := e.store.RecordActionEvent(ctx, key, account.ID, "keepalive", "attempting", "")
		if recordErr != nil {
			return "", recordErr
		}
		if fresh {
			effectiveShutdownMode := resolveShutdownMode(account, config)
			if err = e.provider.ControlInstance(ctx, account, secret, "start", effectiveShutdownMode); err != nil {
				_ = e.store.DeleteActionEvent(ctx, key)
				return "", err
			}
			status = domain.StatusStarting
			_ = e.store.UpdateRuntime(ctx, account.ID, traffic, status, time.Now().UTC())
			_ = e.store.UpdateKeepAliveAt(ctx, account.ID, time.Now().UTC())
			actions = append(actions, "keepalive_start")
			event := newEvent("keepalive", "实例保活启动", "检测到实例在允许运行时段意外停止，已发送启动指令。", account.ID, map[string]string{"账号": masked(account.AccessKeyID), "实例": account.InstanceID})
			_ = e.store.AddOutbox(ctx, event, notify.EnabledChannels(config))
		}
	}

	if config.EnableBilling {
		var balance aliyun.BillingBalance
		balanceCached, _ := e.store.BillingCache(ctx, account.ID, "balance", "", 6*time.Hour, &balance)
		billCached := true
		if account.InstanceID != "" {
			var bill aliyun.BillingBill
			billCached, _ = e.store.BillingCache(ctx, account.ID, "instance_bill", now.Format("2006-01"), 6*time.Hour, &bill)
		}
		if force || now.Hour()%6 == 0 || !balanceCached || !billCached {
			if billingErr := e.refreshBilling(ctx, account, secret, now); billingErr != nil {
				_ = e.store.AddLog(ctx, "error", fmt.Sprintf("账单查询失败 [%s]: %v", masked(account.AccessKeyID), billingErr))
			}
		}
	}
	message := fmt.Sprintf("[%s] 流量 %.2fGB / %.2fGB (%.2f%%) · 状态 %s", masked(account.AccessKeyID), traffic, account.MaxTraffic, percentage, status)
	if len(actions) > 0 {
		message += " · 动作 " + strings.Join(actions, ",")
	}
	_ = e.store.AddLog(ctx, "heartbeat", message)
	return message, nil
}

func (e *Engine) executeScheduledAction(ctx context.Context, config domain.Config, account domain.Account, secret, action string, now time.Time) (bool, error) {
	key := scheduleActionKey(account, scheduleCycleDate(now, account.StartTime, account.StopTime), action)
	fresh, err := e.store.RecordActionEvent(ctx, key, account.ID, "schedule_"+action, "attempting", "")
	if err != nil || !fresh {
		return false, err
	}
	effectiveShutdownMode := resolveShutdownMode(account, config)
	if err = e.provider.ControlInstance(ctx, account, secret, action, effectiveShutdownMode); err != nil {
		_ = e.store.DeleteActionEvent(ctx, key)
		return false, err
	}
	status := domain.StatusStarting
	if action == "stop" {
		status = domain.StatusStopping
	}
	_ = e.store.UpdateRuntime(ctx, account.ID, account.TrafficUsed, status, time.Now().UTC())
	_ = e.store.AddLog(ctx, "info", fmt.Sprintf("执行定时%s [%s]", map[string]string{"start": "开机", "stop": "关机"}[action], masked(account.AccessKeyID)))
	if config.EnableScheduleMail {
		event := newEvent("schedule", "定时任务已执行", fmt.Sprintf("实例定时%s指令已发送。", map[string]string{"start": "开机", "stop": "关机"}[action]), account.ID, map[string]string{"账号": masked(account.AccessKeyID), "实例": account.InstanceID})
		_ = e.store.AddOutbox(ctx, event, notify.EnabledChannels(config))
	}
	return true, nil
}

func (e *Engine) executeCronScheduledAction(ctx context.Context, config domain.Config, account domain.Account, secret, action, expression string, occurrence time.Time) (bool, error) {
	key := fmt.Sprintf("cron_schedule:%d:%s:%s", account.ID, occurrence.Format("200601021504"), action)
	fresh, err := e.store.RecordActionEvent(ctx, key, account.ID, "cron_schedule_"+action, "attempting", expression)
	if err != nil || !fresh {
		return false, err
	}
	if err = e.provider.ControlInstance(ctx, account, secret, action, resolveShutdownMode(account, config)); err != nil {
		_ = e.store.DeleteActionEvent(ctx, key)
		return false, err
	}
	status := domain.StatusStarting
	if action == "stop" {
		status = domain.StatusStopping
	}
	_ = e.store.UpdateRuntime(ctx, account.ID, account.TrafficUsed, status, time.Now().UTC())
	_ = e.store.AddLog(ctx, "info", fmt.Sprintf("执行 Cron 定时%s [%s] (%s)", map[string]string{"start": "开机", "stop": "关机"}[action], masked(account.AccessKeyID), expression))
	if config.EnableScheduleMail {
		event := newEvent("schedule", "定时任务已执行", fmt.Sprintf("实例 Cron 定时%s指令已发送。", map[string]string{"start": "开机", "stop": "关机"}[action]), account.ID, map[string]string{"账号": masked(account.AccessKeyID), "实例": account.InstanceID, "Cron": expression})
		_ = e.store.AddOutbox(ctx, event, notify.EnabledChannels(config))
	}
	return true, nil
}

func (e *Engine) control(ctx context.Context, accountID int64, action, source string) (string, error) {
	lock := e.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	account, err := e.store.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	config, err := e.store.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	action = strings.ToLower(action)
	if action != "start" && action != "stop" {
		return "", errors.New("action must be start or stop")
	}
	if transient(account.InstanceStatus) {
		return "", fmt.Errorf("instance is currently %s", account.InstanceStatus)
	}
	if resolveKeepAlive(account, config) && action == "stop" {
		return "", errors.New("manual shutdown is disabled while keep-alive is enabled")
	}
	secret, err := e.store.AccountSecret(ctx, accountID)
	if err != nil {
		return "", err
	}
	effectiveShutdownMode := resolveShutdownMode(account, config)
	if err = e.provider.ControlInstance(ctx, account, secret, action, effectiveShutdownMode); err != nil {
		return "", err
	}
	status := domain.StatusStarting
	if action == "stop" {
		status = domain.StatusStopping
	}
	if err = e.store.UpdateRuntime(ctx, account.ID, account.TrafficUsed, status, time.Now().UTC()); err != nil {
		return "", err
	}
	message := fmt.Sprintf("%s控制实例 [%s]：%s", source, masked(account.AccessKeyID), action)
	_ = e.store.AddLog(ctx, "audit", message)
	return message, nil
}

func (e *Engine) accountLock(accountID int64) *sync.Mutex {
	value, _ := e.accountLocks.LoadOrStore(accountID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (e *Engine) refreshBilling(ctx context.Context, account domain.Account, secret string, now time.Time) error {
	cycle := now.Format("2006-01")
	setBillingError := func(err error) {
		_ = e.store.SetBillingCache(ctx, account.ID, "error", "", map[string]string{"message": err.Error()})
	}
	var balance aliyun.BillingBalance
	cached, _ := e.store.BillingCache(ctx, account.ID, "balance", "", 6*time.Hour, &balance)
	if !cached {
		value, err := e.provider.GetAccountBalance(ctx, account, secret)
		if err != nil {
			setBillingError(err)
			return err
		}
		balance = value
		_ = e.store.SetBillingCache(ctx, account.ID, "balance", "", balance)
	}
	if account.InstanceID != "" {
		var bill aliyun.BillingBill
		cached, _ = e.store.BillingCache(ctx, account.ID, "instance_bill", cycle, 6*time.Hour, &bill)
		if !cached {
			value, err := e.provider.GetInstanceBill(ctx, account, secret, cycle)
			if err != nil {
				setBillingError(err)
				return err
			}
			_ = e.store.SetBillingCache(ctx, account.ID, "instance_bill", cycle, value)
		}
	}
	_ = e.store.SetBillingCache(ctx, account.ID, "error", "", map[string]string{"message": ""})
	return nil
}

func (e *Engine) notificationWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				item, err := e.store.ClaimOutbox(ctx)
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				if err != nil {
					e.logger.Error("claim notification", "error", err)
					break
				}
				var event domain.NotificationEvent
				if err = json.Unmarshal([]byte(item.Payload), &event); err == nil {
					var config domain.Config
					config, err = e.store.GetConfig(ctx)
					if err == nil {
						sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
						err = e.notify.Send(sendCtx, item.Channel, event, config)
						cancel()
					}
				}
				if err != nil {
					_ = e.store.FailOutbox(ctx, item, err)
					continue
				}
				_ = e.store.CompleteOutbox(ctx, item.ID)
			}
		}
	}
}

func (e *Engine) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func dueWithin(now time.Time, hhmm string, window time.Duration) bool {
	parsed, err := parseClockTime(hhmm)
	if err != nil {
		return false
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	delta := now.Sub(target)
	return delta >= 0 && delta <= window
}

func parseClockTime(value string) (time.Time, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "：", ":"))
	if value == "24:00" {
		return time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC), nil
	}
	return time.Parse("15:04", value)
}

func overnightSchedule(start, stop string) bool {
	startTime, startErr := parseClockTime(start)
	stopTime, stopErr := parseClockTime(stop)
	if startErr != nil || stopErr != nil {
		return false
	}
	return startTime.Hour()*60+startTime.Minute() >= stopTime.Hour()*60+stopTime.Minute()
}

// scheduleCycleDate identifies the date on which a schedule window started.
// For an overnight window, all times before today's start belong to yesterday's cycle.
func scheduleCycleDate(now time.Time, start, stop string) string {
	if overnightSchedule(start, stop) {
		startTime, err := parseClockTime(start)
		if err == nil && now.Hour()*60+now.Minute() < startTime.Hour()*60+startTime.Minute() {
			return now.AddDate(0, 0, -1).Format("2006-01-02")
		}
	}
	return now.Format("2006-01-02")
}

func compactDate(value string) string { return strings.ReplaceAll(value, "-", "") }

func scheduleTimeKey(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "：", ":"))
	if value == "24:00" {
		return "00:00"
	}
	if parsed, err := time.Parse("15:04", value); err == nil {
		return parsed.Format("15:04")
	}
	return value
}

func scheduleActionKey(account domain.Account, cycleDate, action string) string {
	configuredTime := account.StartTime
	if action == "stop" {
		configuredTime = account.StopTime
	}
	return fmt.Sprintf("schedule:%d:%s:%s:%s", account.ID, compactDate(cycleDate), action, scheduleTimeKey(configuredTime))
}

func dailyReportKey(account domain.Account, cycleDate string) string {
	configuredTime := account.DailyReportTime
	if usesSimpleSchedule(account) {
		configuredTime = account.StopTime
		if configuredTime == "" {
			configuredTime = "23:30"
		}
	} else if configuredTime == "" {
		configuredTime = "00:00"
	}
	return fmt.Sprintf("daily_report:%d:%s:%s", account.ID, compactDate(cycleDate), scheduleTimeKey(configuredTime))
}

func usesSimpleSchedule(account domain.Account) bool {
	if !account.ScheduleEnabled {
		return false
	}
	if account.ScheduleMode == "simple" {
		return true
	}
	return account.ScheduleMode != "advanced" && account.StartCron == "" && account.StopCron == ""
}

func scheduledStopSnapshotReady(snap store.TrafficSnapshot, configuredStop string) bool {
	if snap.StopTraffic < 0 {
		return false
	}
	actual, actualErr := parseClockTime(snap.StopTime)
	target, targetErr := parseClockTime(configuredStop)
	if actualErr != nil || targetErr != nil {
		return false
	}
	actualMinute := actual.Hour()*60 + actual.Minute()
	targetMinute := target.Hour()*60 + target.Minute()
	delta := (actualMinute - targetMinute + 24*60) % (24 * 60)
	return delta <= 10
}

func inTimeRange(current, start, end string) bool {
	if start == "" || end == "" {
		return false
	}
	if start < end {
		return current >= start && current < end
	}
	return current >= start || current < end
}

func transient(status string) bool {
	return status == domain.StatusStarting || status == domain.StatusStopping || status == "Pending" || status == domain.StatusUnknown
}

func usagePercent(traffic, maxTraffic float64) float64 {
	if maxTraffic <= 0 {
		return 0
	}
	return math.Round((traffic/maxTraffic)*10000) / 100
}

func masked(accessKeyID string) string {
	if len(accessKeyID) <= 7 {
		return accessKeyID + "***"
	}
	return accessKeyID[:7] + "***"
}

func newEvent(eventType, title, summary string, accountID int64, fields map[string]string) domain.NotificationEvent {
	id, _ := security.NewToken(18)
	return domain.NotificationEvent{ID: id, Type: eventType, Title: title, Summary: summary, AccountID: accountID, Fields: fields, CreatedAt: time.Now().UTC()}
}

func (e *Engine) Summary(ctx context.Context) ([]domain.AccountSummary, time.Time, error) {
	config, err := e.store.GetConfig(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	lastRun, err := e.store.LastMonitorRun(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	result := make([]domain.AccountSummary, 0, len(config.Accounts))
	for _, account := range config.Accounts {
		mode := account.ScheduleMode
		if mode == "" {
			mode = "simple"
			if account.StartCron != "" || account.StopCron != "" {
				mode = "advanced"
			}
		}
		percentage := usagePercent(account.TrafficUsed, account.MaxTraffic)
		item := domain.AccountSummary{
			ID: account.ID, Account: masked(account.AccessKeyID), Remark: account.Remark, Region: account.RegionID, RegionName: RegionName(account.RegionID),
			FlowTotal: account.MaxTraffic, FlowUsed: math.Round(account.TrafficUsed*100) / 100, Percentage: percentage, Threshold: config.TrafficThreshold,
			OverThreshold: percentage >= float64(config.TrafficThreshold), InstanceStatus: account.InstanceStatus, PublicIP: account.PublicIP, PublicIPv6: account.PublicIPv6, LastUpdated: account.UpdatedAt,
			Stale:     account.UpdatedAt.IsZero() || time.Since(account.UpdatedAt) > time.Duration(max(config.APIInterval*2, 180))*time.Second,
			KeepAlive: account.KeepAlive, ShutdownMode: account.ShutdownMode, ScheduleEnabled: account.ScheduleEnabled, ScheduleMode: mode, StartTime: account.StartTime, StopTime: account.StopTime, DailyReport: account.DailyReport, DailyReportTime: account.DailyReportTime,
		}
		if !account.PublicIPUpdatedAt.IsZero() {
			ipUpdatedAt := account.PublicIPUpdatedAt
			item.IPUpdatedAt = &ipUpdatedAt
		}
		if config.EnableBilling {
			var billingError struct {
				Message string `json:"message"`
			}
			if ok, _ := e.store.BillingCache(ctx, account.ID, "error", "", 7*24*time.Hour, &billingError); ok {
				item.BillingError = strings.TrimSpace(billingError.Message)
			}
			var balance aliyun.BillingBalance
			if ok, _ := e.store.BillingCache(ctx, account.ID, "balance", "", 7*24*time.Hour, &balance); ok {
				item.Balance, item.Currency = &balance.Amount, balance.Currency
			}
			var bill aliyun.BillingBill
			if ok, _ := e.store.BillingCache(ctx, account.ID, "instance_bill", time.Now().Format("2006-01"), 7*24*time.Hour, &bill); ok {
				item.MonthlyCost = &bill.TotalCost
			}
		}
		result = append(result, item)
	}
	return result, lastRun, nil
}

func RegionName(region string) string {
	names := map[string]string{
		"cn-hongkong": "中国香港", "ap-southeast-1": "新加坡", "us-west-1": "美国（硅谷）", "us-east-1": "美国（弗吉尼亚）",
		"cn-hangzhou": "华东 1（杭州）", "cn-shanghai": "华东 2（上海）", "cn-qingdao": "华北 1（青岛）", "cn-beijing": "华北 2（北京）",
		"cn-zhangjiakou": "华北 3（张家口）", "cn-huhehaote": "华北 5（呼和浩特）", "cn-wulanchabu": "华北 6（乌兰察布）",
		"cn-shenzhen": "华南 1（深圳）", "cn-heyuan": "华南 2（河源）", "cn-guangzhou": "华南 3（广州）", "cn-chengdu": "西南 1（成都）", "ap-northeast-1": "日本（东京）", "ap-northeast-2": "韩国（首尔）",
	}
	if name := names[region]; name != "" {
		return name
	}
	return region
}

func ParseControlPayload(action, source string) string {
	payload, _ := json.Marshal(map[string]string{"action": strings.ToLower(action), "source": source})
	return string(payload)
}

func ParseNotifyPayload(channel string) string {
	payload, _ := json.Marshal(map[string]string{"channel": channel})
	return string(payload)
}

func JobUniqueKey(jobType string, accountID int64, suffix string) string {
	return jobType + ":" + strconv.FormatInt(accountID, 10) + ":" + suffix
}

func (e *Engine) checkDailyReport(ctx context.Context, now time.Time) {
	config, err := e.store.GetConfig(ctx)
	if err != nil || !config.EnableDailyReport {
		return
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	localNow := now.In(location)
	unscheduledGroups := make(map[string][]domain.Account)

	for _, acc := range config.Accounts {
		if acc.DailyReport != nil && !*acc.DailyReport {
			continue
		}
		if !usesSimpleSchedule(acc) {
			targetTime := acc.DailyReportTime
			if targetTime == "" {
				targetTime = "00:00"
			}
			targetTime = scheduleTimeKey(targetTime)
			unscheduledGroups[targetTime] = append(unscheduledGroups[targetTime], acc)
			continue
		}
		targetTime := acc.StopTime
		if targetTime == "" {
			targetTime = "23:30"
		}
		if !dueWithin(localNow, targetTime, 10*time.Minute) {
			continue
		}
		cycleDate := scheduleCycleDate(localNow, acc.StartTime, acc.StopTime)
		// The stop monitor writes this snapshot after its final traffic
		// query. It normally queues the report too; this recovers a crash
		// between the snapshot write and the enqueue.
		snap, err := e.store.GetTrafficSnapshot(ctx, acc.ID, cycleDate)
		if err != nil || !scheduledStopSnapshotReady(snap, targetTime) {
			continue
		}
		key := dailyReportKey(acc, compactDate(cycleDate))
		fresh, err := e.store.RecordActionEvent(ctx, key, acc.ID, "daily_report", "attempting", "")
		if err != nil || !fresh {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"force": false, "account_id": acc.ID})
		if _, err = e.Enqueue(ctx, JobDailyReport, acc.ID, string(payload), key); err != nil {
			_ = e.store.DeleteActionEvent(ctx, key)
		}
	}

	groupTimes := make([]string, 0, len(unscheduledGroups))
	for targetTime := range unscheduledGroups {
		groupTimes = append(groupTimes, targetTime)
	}
	slices.Sort(groupTimes)
	for _, targetTime := range groupTimes {
		if !dueWithin(localNow, targetTime, 10*time.Minute) {
			continue
		}
		dateKey := localNow.Format("20060102")
		var freshAccounts []domain.Account
		var claimErr error
		for _, acc := range unscheduledGroups[targetTime] {
			key := dailyReportKey(acc, dateKey)
			fresh, err := e.store.RecordActionEvent(ctx, key, acc.ID, "daily_report", "attempting", "")
			if err != nil {
				claimErr = err
				break
			}
			if fresh {
				freshAccounts = append(freshAccounts, acc)
			}
		}
		if claimErr != nil {
			for _, acc := range freshAccounts {
				_ = e.store.DeleteActionEvent(ctx, dailyReportKey(acc, dateKey))
			}
			continue
		}
		if len(freshAccounts) == 0 {
			continue
		}
		if len(freshAccounts) == 1 {
			acc := freshAccounts[0]
			key := dailyReportKey(acc, dateKey)
			payload, _ := json.Marshal(map[string]any{"force": false, "account_id": acc.ID})
			if _, err := e.Enqueue(ctx, JobDailyReport, acc.ID, string(payload), key); err != nil {
				_ = e.store.DeleteActionEvent(ctx, key)
			}
			continue
		}

		key := fmt.Sprintf("daily_report:group:%s:%s", dateKey, targetTime)
		accountIDs := make([]int64, 0, len(freshAccounts))
		for _, acc := range freshAccounts {
			accountIDs = append(accountIDs, acc.ID)
			key += ":" + strconv.FormatInt(acc.ID, 10)
		}
		payload, _ := json.Marshal(map[string]any{"force": false, "account_ids": accountIDs, "report_date": localNow.Format("2006-01-02")})
		if _, err := e.Enqueue(ctx, JobDailyReport, 0, string(payload), key); err != nil {
			for _, acc := range freshAccounts {
				_ = e.store.DeleteActionEvent(ctx, dailyReportKey(acc, dateKey))
			}
		}
	}
}

func (e *Engine) EnqueueDailyReportForAccount(ctx context.Context, force bool, accountID int64) (domain.Job, error) {
	uniqueKey := ""
	if !force {
		minute := time.Now().UTC().Format("200601021504")
		uniqueKey = fmt.Sprintf("daily_report:%d:%s", accountID, minute)
	}
	payload, _ := json.Marshal(map[string]any{"force": force, "account_id": accountID})
	return e.Enqueue(ctx, JobDailyReport, accountID, string(payload), uniqueKey)
}

func (e *Engine) EnqueueDailyReport(ctx context.Context, force bool) (domain.Job, error) {
	return e.EnqueueDailyReportForAccount(ctx, force, 0)
}

func (e *Engine) findStartTrafficFor24Hours(ctx context.Context, accountID int64, now time.Time) float64 {
	if traffic, ok := e.store.Traffic24HoursAgo(ctx, accountID, now); ok {
		return traffic
	}
	if traffic, ok := e.store.EarliestTrafficSince(ctx, accountID, now.Add(-24*time.Hour).Unix()); ok {
		return traffic
	}
	return 0
}

func (e *Engine) findStartTrafficForSchedule(ctx context.Context, accountID int64, now time.Time, startTime, stopTime string) float64 {
	parsed, err := parseClockTime(startTime)
	if err != nil {
		return e.findStartTrafficFor24Hours(ctx, accountID, now)
	}
	date := now
	if overnightSchedule(startTime, stopTime) && now.Hour()*60+now.Minute() < parsed.Hour()*60+parsed.Minute() {
		date = now.AddDate(0, 0, -1)
	}
	target := time.Date(date.Year(), date.Month(), date.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	if traffic, ok := e.store.TrafficAroundTime(ctx, accountID, target); ok {
		return traffic
	}
	return e.findStartTrafficFor24Hours(ctx, accountID, now)
}

func formatTrafficGB(gb float64) string {
	if gb < 0 {
		return "0.00 GB"
	}
	if gb < 1.0 && gb > 0 {
		rounded3 := math.Round(gb*1000) / 1000
		if math.Abs(rounded3-math.Round(gb*100)/100) > 1e-4 {
			return fmt.Sprintf("%.3f GB", rounded3)
		}
	}
	return fmt.Sprintf("%.2f GB", gb)
}

func (e *Engine) calculateInstanceConsumption(ctx context.Context, acc domain.Account, now time.Time, dateStr string, force bool) (float64, string) {
	if usesSimpleSchedule(acc) && !force {
		dateStr = scheduleCycleDate(now, acc.StartTime, acc.StopTime)
	}
	snap := store.TrafficSnapshot{StartTraffic: -1, StopTraffic: -1}
	if stored, err := e.store.GetTrafficSnapshot(ctx, acc.ID, dateStr); err == nil {
		snap = stored
	} else if !errors.Is(err, sql.ErrNoRows) {
		_ = e.store.AddLog(ctx, "error", fmt.Sprintf("日报快照读取失败 [%s]: %v", masked(acc.AccessKeyID), err))
	}
	consumed := 0.0
	periodDesc := ""
	if force {
		periodDesc = "测试推送（前24小时）"
		startTraffic := e.findStartTrafficFor24Hours(ctx, acc.ID, now)
		consumed = acc.TrafficUsed - startTraffic
	} else if usesSimpleSchedule(acc) {
		periodDesc = fmt.Sprintf("定时时段 (%s ~ %s)", acc.StartTime, acc.StopTime)
		startTraffic := snap.StartTraffic
		if startTraffic < 0 {
			startTraffic = e.findStartTrafficForSchedule(ctx, acc.ID, now, acc.StartTime, acc.StopTime)
		}
		if snap.StopTraffic >= 0 {
			consumed = snap.StopTraffic - startTraffic
		} else {
			consumed = acc.TrafficUsed - startTraffic
		}
	} else {
		periodDesc = "前24小时运行"
		startTraffic := e.findStartTrafficFor24Hours(ctx, acc.ID, now)
		consumed = acc.TrafficUsed - startTraffic
	}
	if acc.TrafficUsed < 0 {
		consumed = 0
	}
	if consumed < 0 {
		if now.Day() == 1 || now.Day() == 2 {
			consumed = acc.TrafficUsed
		} else {
			consumed = 0
		}
	}
	consumed = math.Round(consumed*1000) / 1000
	return consumed, periodDesc
}

// Refresh the persisted traffic value before building a report. Standalone
// reports may be queued before the regular monitor cycle, so the stored value
// may still be an older sample.
func (e *Engine) refreshTrafficForReport(ctx context.Context, account domain.Account, now time.Time, maxAge time.Duration) (domain.Account, error) {
	if e.provider == nil {
		return account, nil
	}
	if maxAge < 0 {
		maxAge = 10 * time.Minute
	}
	if account.TrafficUsed > 0 && !account.UpdatedAt.IsZero() {
		age := time.Since(account.UpdatedAt)
		if age >= 0 && age < maxAge {
			return account, nil
		}
	}
	lock := e.accountLock(account.ID)
	lock.Lock()
	defer lock.Unlock()
	// A monitor job may have refreshed the account while this report waited
	// for its per-account lock.
	latest, err := e.store.GetAccount(ctx, account.ID)
	if err == nil && latest.TrafficUsed > 0 && !latest.UpdatedAt.IsZero() && time.Since(latest.UpdatedAt) < maxAge {
		return latest, nil
	}

	secret, err := e.store.AccountSecret(ctx, account.ID)
	if err != nil {
		return account, err
	}
	traffic, err := e.provider.GetTraffic(ctx, account, secret)
	if err != nil {
		_ = e.store.AddLog(ctx, "error", fmt.Sprintf("日报流量查询失败 [%s]: %v", masked(account.AccessKeyID), err))
		return account, err
	}
	updatedAt := time.Now().UTC()
	if err = e.store.UpdateRuntime(ctx, account.ID, traffic, account.InstanceStatus, updatedAt); err != nil {
		return account, err
	}
	_ = e.store.AddTrafficStats(ctx, account.ID, traffic, now)
	account.TrafficUsed = traffic
	account.UpdatedAt = updatedAt
	return account, nil
}

func (e *Engine) refreshTrafficForDailyReport(ctx context.Context, account domain.Account, now time.Time, maxAge time.Duration, force bool) (domain.Account, error) {
	if usesSimpleSchedule(account) && !force {
		cycleDate := scheduleCycleDate(now, account.StartTime, account.StopTime)
		snap, err := e.store.GetTrafficSnapshot(ctx, account.ID, cycleDate)
		if err != nil || snap.StopTraffic < 0 {
			// A missing stop sample may mean the shutdown query failed. Retry the
			// provider even when an earlier account sample is still cache-fresh.
			maxAge = 0
		}
	}
	return e.refreshTrafficForReport(ctx, account, now, maxAge)
}

type instanceReportItem struct {
	account     domain.Account
	consumed    float64
	periodDesc  string
	monthlyCost *float64
	balance     *float64
	currency    string
}

func (e *Engine) generateAndSendDailyReport(ctx context.Context, force bool, targetAccountID ...int64) (string, error) {
	var targetID int64
	if len(targetAccountID) > 0 {
		targetID = targetAccountID[0]
	}
	return e.generateAndSendDailyReportSelected(ctx, force, targetID, nil, "", "")
}

func (e *Engine) generateAndSendDailyReportForAccounts(ctx context.Context, accountIDs []int64, reportDate, jobID string) (string, error) {
	if len(accountIDs) == 0 {
		return "", errors.New("daily report group has no accounts")
	}
	return e.generateAndSendDailyReportSelected(ctx, false, 0, accountIDs, reportDate, jobID)
}

func (e *Engine) generateAndSendDailyReportSelected(ctx context.Context, force bool, targetID int64, accountIDs []int64, reportDate, jobID string, notificationPreview ...*domain.NotificationConfig) (string, error) {
	config, err := e.store.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	preview := len(notificationPreview) > 0 && notificationPreview[0] != nil
	if preview {
		for _, channel := range []string{"email", "telegram", "webhook"} {
			config = applyNotificationPreview(config, channel, *notificationPreview[0])
		}
	}
	if !force && !config.EnableDailyReport {
		return "daily report disabled", nil
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	now := time.Now().In(location)
	dateStr := now.Format("2006-01-02")
	if parsed, parseErr := time.Parse("2006-01-02", reportDate); parseErr == nil && parsed.Format("2006-01-02") == reportDate {
		dateStr = reportDate
	}

	var selected map[int64]bool
	if accountIDs != nil {
		selected = make(map[int64]bool, len(accountIDs))
		for _, id := range accountIDs {
			selected[id] = true
		}
		remaining := 0
		for _, acc := range config.Accounts {
			if selected[acc.ID] && !usesSimpleSchedule(acc) && (acc.DailyReport == nil || *acc.DailyReport) {
				targetID = acc.ID
				remaining++
			}
		}
		if remaining != 1 {
			targetID = 0
		}
	}

	if targetID > 0 {
		var targetAcc *domain.Account
		for i := range config.Accounts {
			if config.Accounts[i].ID == targetID {
				targetAcc = &config.Accounts[i]
				break
			}
		}
		if targetAcc == nil {
			return "account not found", nil
		}
		if !force && targetAcc.DailyReport != nil && !*targetAcc.DailyReport {
			return "daily report disabled for this account", nil
		}
		refreshed, refreshErr := e.refreshTrafficForDailyReport(ctx, *targetAcc, now, time.Duration(config.APIInterval)*time.Second, force)
		if refreshErr != nil && usesSimpleSchedule(*targetAcc) && !force {
			return "", fmt.Errorf("refresh scheduled report traffic: %w", refreshErr)
		}
		*targetAcc = refreshed

		consumed, periodDesc := e.calculateInstanceConsumption(ctx, *targetAcc, now, dateStr, force)
		instName := targetAcc.Remark
		if instName == "" {
			instName = targetAcc.InstanceID
		}
		if instName == "" {
			instName = masked(targetAcc.AccessKeyID)
		}

		currency := "¥"
		if targetAcc.SiteType == "international" {
			currency = "$"
		}
		var monthlyCost *float64
		var balance *float64
		if config.EnableBilling {
			var bal aliyun.BillingBalance
			if ok, _ := e.store.BillingCache(ctx, targetAcc.ID, "balance", "", 7*24*time.Hour, &bal); ok {
				val := bal.Amount
				balance = &val
				if bal.Currency == "USD" {
					currency = "$"
				}
			}
			var bill aliyun.BillingBill
			if ok, _ := e.store.BillingCache(ctx, targetAcc.ID, "instance_bill", now.Format("2006-01"), 7*24*time.Hour, &bill); ok {
				val := bill.TotalCost
				monthlyCost = &val
			}
		}

		var sb strings.Builder
		sb.WriteString("【CDT Monitor · 实例流量与账单日报】\n")
		sb.WriteString(fmt.Sprintf("📅 统计日期：%s (%s)\n", dateStr, config.Timezone))
		sb.WriteString(fmt.Sprintf("🖥️ 实例名称：%s (%s / %s)\n", instName, RegionName(targetAcc.RegionID), masked(targetAcc.AccessKeyID)))
		sb.WriteString(fmt.Sprintf("⏱️ 运行模式：%s\n", periodDesc))
		sb.WriteString(fmt.Sprintf("📊 消耗流量：%s\n", formatTrafficGB(consumed)))
		sb.WriteString(fmt.Sprintf("📈 当月累计：%s / %.0f GB (%.2f%%)\n", formatTrafficGB(targetAcc.TrafficUsed), targetAcc.MaxTraffic, usagePercent(targetAcc.TrafficUsed, targetAcc.MaxTraffic)))
		if config.EnableBilling {
			costText := "待同步"
			if monthlyCost != nil {
				costText = fmt.Sprintf("%s%.2f", currency, *monthlyCost)
			}
			balText := "待同步"
			if balance != nil {
				balText = fmt.Sprintf("%s%.2f", currency, *balance)
			}
			sb.WriteString(fmt.Sprintf("💰 当月产生费用：%s\n", costText))
			sb.WriteString(fmt.Sprintf("💳 账户可用余额：%s\n", balText))
		}

		fields := map[string]string{
			"统计日期": dateStr,
			"实例名称": instName,
			"运行模式": periodDesc,
			"消耗流量": formatTrafficGB(consumed),
			"当月累计": fmt.Sprintf("%s / %.0f GB", formatTrafficGB(targetAcc.TrafficUsed), targetAcc.MaxTraffic),
		}
		if config.EnableBilling {
			if monthlyCost != nil {
				fields["本月费用"] = fmt.Sprintf("%s%.2f", currency, *monthlyCost)
			}
			if balance != nil {
				fields["账户余额"] = fmt.Sprintf("%s%.2f", currency, *balance)
			}
		}

		title := fmt.Sprintf("CDT Monitor · 实例 [%s] 流量与账单日报 (%s)", instName, dateStr)
		event := newEvent("daily_report", title, sb.String(), targetAcc.ID, fields)
		if jobID != "" {
			event.ID = "daily_report_group:" + jobID
		}
		channels := notify.EnabledChannels(config)
		if len(channels) == 0 {
			msg := fmt.Sprintf("实例 [%s] 日报已生成，但未启用任何通知渠道 (消耗: %s)", instName, formatTrafficGB(consumed))
			_ = e.store.AddLog(ctx, "info", msg)
			return msg, nil
		}
		if err = e.deliverDailyReport(ctx, config, event, channels, preview); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("实例 [%s] 流量与账单日报已发送 (消耗: %s)", instName, formatTrafficGB(consumed))
		_ = e.store.AddLog(ctx, "info", msg)
		return msg, nil
	}

	var items []instanceReportItem
	var excludedCount int
	for _, acc := range config.Accounts {
		if selected != nil && (!selected[acc.ID] || usesSimpleSchedule(acc)) {
			continue
		}
		if acc.DailyReport != nil && !*acc.DailyReport {
			excludedCount++
			continue
		}
		refreshed, refreshErr := e.refreshTrafficForDailyReport(ctx, acc, now, time.Duration(config.APIInterval)*time.Second, force)
		if refreshErr != nil && usesSimpleSchedule(acc) && !force {
			return "", fmt.Errorf("refresh scheduled report traffic: %w", refreshErr)
		}
		acc = refreshed
		consumed, periodDesc := e.calculateInstanceConsumption(ctx, acc, now, dateStr, force)

		item := instanceReportItem{
			account:    acc,
			consumed:   consumed,
			periodDesc: periodDesc,
			currency:   "¥",
		}
		if acc.SiteType == "international" {
			item.currency = "$"
		}
		if config.EnableBilling {
			var bal aliyun.BillingBalance
			if ok, _ := e.store.BillingCache(ctx, acc.ID, "balance", "", 7*24*time.Hour, &bal); ok {
				val := bal.Amount
				item.balance = &val
				if bal.Currency == "USD" {
					item.currency = "$"
				}
			}
			var bill aliyun.BillingBill
			if ok, _ := e.store.BillingCache(ctx, acc.ID, "instance_bill", now.Format("2006-01"), 7*24*time.Hour, &bill); ok {
				val := bill.TotalCost
				item.monthlyCost = &val
			}
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		if selected != nil {
			msg := "该时段没有可生成日报的实例，未发送日报"
			_ = e.store.AddLog(ctx, "info", msg)
			return msg, nil
		}
		if excludedCount > 0 {
			msg := "所有实例已关闭日报推送，未发送日报"
			_ = e.store.AddLog(ctx, "info", msg)
			return msg, nil
		}
		msg := "当前未配置任何实例，未发送日报"
		_ = e.store.AddLog(ctx, "info", msg)
		return msg, nil
	}

	totalConsumed := 0.0
	totalMonthTraffic := 0.0
	totalMaxTraffic := 0.0
	totalCostCNY := 0.0
	totalCostUSD := 0.0
	totalBalanceCNY := 0.0
	totalBalanceUSD := 0.0
	hasCost := false
	hasBalance := false

	for _, it := range items {
		totalConsumed += it.consumed
		totalMonthTraffic += it.account.TrafficUsed
		totalMaxTraffic += it.account.MaxTraffic
		if it.monthlyCost != nil {
			hasCost = true
			if it.currency == "$" {
				totalCostUSD += *it.monthlyCost
			} else {
				totalCostCNY += *it.monthlyCost
			}
		}
		if it.balance != nil {
			hasBalance = true
			if it.currency == "$" {
				totalBalanceUSD += *it.balance
			} else {
				totalBalanceCNY += *it.balance
			}
		}
	}
	totalConsumed = math.Round(totalConsumed*100) / 100
	totalMonthTraffic = math.Round(totalMonthTraffic*100) / 100
	totalMaxTraffic = math.Round(totalMaxTraffic*100) / 100

	var sb strings.Builder
	sb.WriteString("【CDT Monitor 每日消费与流量日报】\n")
	sb.WriteString(fmt.Sprintf("📅 统计日期：%s (%s)\n", dateStr, config.Timezone))
	sb.WriteString(fmt.Sprintf("🖥️ 纳入实例：%d 台", len(items)))
	if excludedCount > 0 {
		sb.WriteString(fmt.Sprintf("（已排除 %d 台未开启日报实例）", excludedCount))
	}
	sb.WriteString("\n\n📊 汇总统计：\n")
	sb.WriteString(fmt.Sprintf("• 今日/时段消耗流量总和：%s\n", formatTrafficGB(totalConsumed)))
	sb.WriteString(fmt.Sprintf("• 当月累计使用流量总和：%s / %.0f GB", formatTrafficGB(totalMonthTraffic), totalMaxTraffic))
	if totalMaxTraffic > 0 {
		sb.WriteString(fmt.Sprintf(" (%.2f%%)", (totalMonthTraffic/totalMaxTraffic)*100))
	}
	sb.WriteString("\n")
	if config.EnableBilling {
		if hasCost {
			costStr := ""
			if totalCostCNY > 0 && totalCostUSD > 0 {
				costStr = fmt.Sprintf("¥%.2f + $%.2f", totalCostCNY, totalCostUSD)
			} else if totalCostUSD > 0 {
				costStr = fmt.Sprintf("$%.2f", totalCostUSD)
			} else {
				costStr = fmt.Sprintf("¥%.2f", totalCostCNY)
			}
			sb.WriteString(fmt.Sprintf("• 当月产生总费用：%s\n", costStr))
		}
		if hasBalance {
			balStr := ""
			if totalBalanceCNY > 0 && totalBalanceUSD > 0 {
				balStr = fmt.Sprintf("¥%.2f + $%.2f", totalBalanceCNY, totalBalanceUSD)
			} else if totalBalanceUSD > 0 {
				balStr = fmt.Sprintf("$%.2f", totalBalanceUSD)
			} else {
				balStr = fmt.Sprintf("¥%.2f", totalBalanceCNY)
			}
			sb.WriteString(fmt.Sprintf("• 账户可用总余额：%s\n", balStr))
		}
	}

	sb.WriteString("\n📋 各实例详情：\n")
	for idx, it := range items {
		instName := it.account.Remark
		if instName == "" {
			instName = it.account.InstanceID
		}
		if instName == "" {
			instName = masked(it.account.AccessKeyID)
		}
		sb.WriteString(fmt.Sprintf("%d. %s (%s / %s)\n", idx+1, instName, RegionName(it.account.RegionID), masked(it.account.AccessKeyID)))
		sb.WriteString(fmt.Sprintf("   • 运行模式：%s\n", it.periodDesc))
		sb.WriteString(fmt.Sprintf("   • 消耗流量：%s\n", formatTrafficGB(it.consumed)))
		sb.WriteString(fmt.Sprintf("   • 当月累计：%s / %.0f GB (%.2f%%)\n", formatTrafficGB(it.account.TrafficUsed), it.account.MaxTraffic, usagePercent(it.account.TrafficUsed, it.account.MaxTraffic)))
		if config.EnableBilling {
			costText := "待同步"
			if it.monthlyCost != nil {
				costText = fmt.Sprintf("%s%.2f", it.currency, *it.monthlyCost)
			}
			balText := "待同步"
			if it.balance != nil {
				balText = fmt.Sprintf("%s%.2f", it.currency, *it.balance)
			}
			sb.WriteString(fmt.Sprintf("   • 本月费用：%s | 账户余额：%s\n", costText, balText))
		}
	}
	fields := map[string]string{
		"统计日期":   dateStr,
		"纳入实例":   fmt.Sprintf("%d 台", len(items)),
		"流量消耗总和": formatTrafficGB(totalConsumed),
		"当月累计流量": fmt.Sprintf("%s / %.0f GB", formatTrafficGB(totalMonthTraffic), totalMaxTraffic),
	}
	if excludedCount > 0 {
		fields["排除实例"] = fmt.Sprintf("%d 台", excludedCount)
	}
	if config.EnableBilling {
		if hasCost {
			if totalCostUSD > 0 && totalCostCNY > 0 {
				fields["本月费用总和"] = fmt.Sprintf("¥%.2f + $%.2f", totalCostCNY, totalCostUSD)
			} else if totalCostUSD > 0 {
				fields["本月费用总和"] = fmt.Sprintf("$%.2f", totalCostUSD)
			} else {
				fields["本月费用总和"] = fmt.Sprintf("¥%.2f", totalCostCNY)
			}
		}
		if hasBalance {
			if totalBalanceUSD > 0 && totalBalanceCNY > 0 {
				fields["账户余额总和"] = fmt.Sprintf("¥%.2f + $%.2f", totalBalanceCNY, totalBalanceUSD)
			} else if totalBalanceUSD > 0 {
				fields["账户余额总和"] = fmt.Sprintf("$%.2f", totalBalanceUSD)
			} else {
				fields["账户余额总和"] = fmt.Sprintf("¥%.2f", totalBalanceCNY)
			}
		}
	}
	for idx, it := range items {
		instName := it.account.Remark
		if instName == "" {
			instName = it.account.InstanceID
		}
		if instName == "" {
			instName = masked(it.account.AccessKeyID)
		}
		billDetail := ""
		if config.EnableBilling && it.monthlyCost != nil {
			billDetail = fmt.Sprintf(" | 费用: %s%.2f", it.currency, *it.monthlyCost)
		}
		fields[fmt.Sprintf("实例%d [%s]", idx+1, instName)] = fmt.Sprintf("消耗: %s (%s) | 累计: %s/%.0f GB%s",
			formatTrafficGB(it.consumed), it.periodDesc, formatTrafficGB(it.account.TrafficUsed), it.account.MaxTraffic, billDetail)
	}

	title := fmt.Sprintf("CDT Monitor · 每日流量与账单日报 (%s)", dateStr)
	event := newEvent("daily_report", title, sb.String(), 0, fields)
	if jobID != "" {
		event.ID = "daily_report_group:" + jobID
	}
	channels := notify.EnabledChannels(config)
	if len(channels) == 0 {
		msg := fmt.Sprintf("日报已生成，但未启用任何通知通道 (消耗总和: %s)", formatTrafficGB(totalConsumed))
		_ = e.store.AddLog(ctx, "info", msg)
		return msg, nil
	}
	if err = e.deliverDailyReport(ctx, config, event, channels, preview); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("每日流量与账单日报已发送 (纳入 %d 台实例，消耗总和: %s)", len(items), formatTrafficGB(totalConsumed))
	_ = e.store.AddLog(ctx, "info", msg)
	return msg, nil
}

func (e *Engine) deliverDailyReport(ctx context.Context, config domain.Config, event domain.NotificationEvent, channels []string, preview bool) error {
	if !preview {
		return e.store.AddOutbox(ctx, event, channels)
	}
	for _, channel := range channels {
		if err := e.notify.Send(ctx, channel, event, config); err != nil {
			return fmt.Errorf("daily report test on %s: %w", channel, err)
		}
	}
	return nil
}
