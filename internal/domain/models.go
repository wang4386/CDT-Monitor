package domain

import "time"

const (
	StatusUnknown  = "Unknown"
	StatusRunning  = "Running"
	StatusStopped  = "Stopped"
	StatusStarting = "Starting"
	StatusStopping = "Stopping"
)

type Account struct {
	ID                int64     `json:"id"`
	AccessKeyID       string    `json:"access_key_id"`
	AccessKeySecret   string    `json:"access_key_secret,omitempty"`
	SecretConfigured  bool      `json:"secret_configured"`
	RegionID          string    `json:"region_id"`
	InstanceID        string    `json:"instance_id"`
	PublicIP          string    `json:"-"`
	PublicIPv6        string    `json:"-"`
	PublicIPUpdatedAt time.Time `json:"-"`
	MaxTraffic        float64   `json:"max_traffic"`
	ScheduleEnabled   bool      `json:"schedule_enabled"`
	ScheduleMode      string    `json:"schedule_mode"`
	StartTime         string    `json:"start_time"`
	StopTime          string    `json:"stop_time"`
	Remark            string    `json:"remark"`
	SiteType          string    `json:"site_type"`
	TrafficUsed       float64   `json:"traffic_used"`
	InstanceStatus    string    `json:"instance_status"`
	UpdatedAt         time.Time `json:"updated_at"`
	LastKeepAliveAt   time.Time `json:"last_keep_alive_at,omitempty"`
	MonthlyCost       *float64  `json:"monthly_cost,omitempty"`
	Balance           *float64  `json:"balance,omitempty"`
	Currency          string    `json:"currency,omitempty"`
	BillingError      string    `json:"billing_error,omitempty"`
	BillingUpdatedAt  time.Time `json:"billing_updated_at,omitempty"`
	KeepAlive         *bool     `json:"keep_alive"`
	ShutdownMode      string    `json:"shutdown_mode"`
	DailyReport       *bool     `json:"daily_report"`
	DailyReportTime   string    `json:"daily_report_time"`
	StartCron         string    `json:"start_cron,omitempty"`
	StopCron          string    `json:"stop_cron,omitempty"`
}

type EmailConfig struct {
	Enabled            bool   `json:"enabled"`
	To                 string `json:"to"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	Password           string `json:"password,omitempty"`
	PasswordConfigured bool   `json:"password_configured"`
	Security           string `json:"security"`
}

type TelegramConfig struct {
	Enabled         bool   `json:"enabled"`
	Token           string `json:"token,omitempty"`
	TokenConfigured bool   `json:"token_configured"`
	ChatID          string `json:"chat_id"`
	ProxyType       string `json:"proxy_type"`
	ProxyURL        string `json:"proxy_url"`
	ProxyIP         string `json:"proxy_ip"`
	ProxyPort       string `json:"proxy_port"`
	ProxyUser       string `json:"proxy_user"`
	ProxyPass       string `json:"proxy_pass,omitempty"`
	ProxyConfigured bool   `json:"proxy_password_configured"`
}

type WebhookConfig struct {
	Enabled          bool   `json:"enabled"`
	URL              string `json:"url"`
	Method           string `json:"method"`
	Type             string `json:"request_type"`
	Headers          string `json:"headers,omitempty"`
	Body             string `json:"body"`
	Provider         string `json:"provider,omitempty"`
	Secret           string `json:"secret,omitempty"`
	SecretConfigured bool   `json:"secret_configured"`
}

type NotificationConfig struct {
	Email    EmailConfig    `json:"email"`
	Telegram TelegramConfig `json:"telegram"`
	Webhook  WebhookConfig  `json:"webhook"`
}

type Config struct {
	AdminPassword      string             `json:"admin_password,omitempty"`
	TrafficThreshold   int                `json:"traffic_threshold"`
	EnableScheduleMail bool               `json:"enable_schedule_notification"`
	ShutdownMode       string             `json:"shutdown_mode"`
	ThresholdAction    string             `json:"threshold_action"`
	KeepAlive          bool               `json:"keep_alive"`
	APIInterval        int                `json:"api_interval"`
	EnableBilling      bool               `json:"enable_billing"`
	Timezone           string             `json:"timezone"`
	EnableDailyReport  bool               `json:"enable_daily_report"`
	DailyReportTime    string             `json:"daily_report_time"`
	Notifications      NotificationConfig `json:"notifications"`
	Accounts           []Account          `json:"accounts"`
}

type AccountSummary struct {
	ID              int64      `json:"id"`
	Account         string     `json:"account"`
	Remark          string     `json:"remark"`
	Region          string     `json:"region"`
	RegionName      string     `json:"region_name"`
	FlowTotal       float64    `json:"flow_total"`
	FlowUsed        float64    `json:"flow_used"`
	Percentage      float64    `json:"percentage"`
	Threshold       int        `json:"threshold"`
	OverThreshold   bool       `json:"over_threshold"`
	InstanceStatus  string     `json:"instance_status"`
	PublicIP        string     `json:"public_ip,omitempty"`
	PublicIPv6      string     `json:"public_ipv6,omitempty"`
	IPUpdatedAt     *time.Time `json:"ip_updated_at,omitempty"`
	LastUpdated     time.Time  `json:"last_updated"`
	Stale           bool       `json:"stale"`
	MonthlyCost     *float64   `json:"monthly_cost,omitempty"`
	Balance         *float64   `json:"balance,omitempty"`
	Currency        string     `json:"currency,omitempty"`
	BillingError    string     `json:"billing_error,omitempty"`
	KeepAlive       *bool      `json:"keep_alive"`
	ShutdownMode    string     `json:"shutdown_mode"`
	ScheduleEnabled bool       `json:"schedule_enabled"`
	ScheduleMode    string     `json:"schedule_mode"`
	StartTime       string     `json:"start_time,omitempty"`
	StopTime        string     `json:"stop_time,omitempty"`
	DailyReport     *bool      `json:"daily_report"`
	DailyReportTime string     `json:"daily_report_time,omitempty"`
}

type Job struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	AccountID   int64     `json:"account_id,omitempty"`
	Payload     string    `json:"-"`
	Status      string    `json:"status"`
	Result      string    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	Attempts    int       `json:"attempts"`
	MaxAttempts int       `json:"max_attempts"`
	AvailableAt time.Time `json:"available_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type Passkey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type LogEntry struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type TrafficPoint struct {
	At      time.Time `json:"at"`
	Traffic float64   `json:"traffic"`
}

type History struct {
	Hourly []TrafficPoint `json:"hourly"`
	Daily  []TrafficPoint `json:"daily"`
}

type NotificationEvent struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Summary   string            `json:"summary"`
	AccountID int64             `json:"account_id,omitempty"`
	Fields    map[string]string `json:"fields"`
	CreatedAt time.Time         `json:"created_at"`
}
