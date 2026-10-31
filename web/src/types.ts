export type Account = {
  id: number
  access_key_id: string
  access_key_secret?: string
  secret_configured: boolean
  region_id: string
  instance_id: string
  public_ip?: string
  public_ipv6?: string
  max_traffic: number
  schedule_enabled: boolean
  schedule_mode?: 'simple' | 'advanced'
  start_time: string
  stop_time: string
  remark: string
  site_type: 'china' | 'international'
  keep_alive?: boolean | null
  shutdown_mode?: '' | 'KeepCharging' | 'StopCharging'
  daily_report?: boolean | null
  daily_report_time?: string
  start_cron?: string
  stop_cron?: string
}

export type Config = {
  admin_password?: string
  traffic_threshold: number
  enable_schedule_notification: boolean
  shutdown_mode: 'KeepCharging' | 'StopCharging'
  threshold_action: 'stop_and_notify' | 'notify_only'
  keep_alive: boolean
  api_interval: number
  enable_billing: boolean
  timezone: string
  enable_daily_report: boolean
  daily_report_time: string
  notifications: {
    email: { enabled: boolean; to: string; host: string; port: number; username: string; password?: string; password_configured: boolean; security: string }
    telegram: { enabled: boolean; token?: string; token_configured: boolean; chat_id: string; proxy_type: string; proxy_url: string; proxy_ip: string; proxy_port: string; proxy_user: string; proxy_pass?: string; proxy_password_configured: boolean }
    webhook: { enabled: boolean; url: string; method: string; request_type: string; headers?: string; body: string; provider?: string; secret?: string; secret_configured?: boolean }
  }
  accounts: Account[]
}

export type AccountSummary = {
  id: number
  account: string
  remark: string
  region: string
  region_name: string
  flow_total: number
  flow_used: number
  percentage: number
  threshold: number
  over_threshold: boolean
  instance_status: string
  public_ip?: string
  public_ipv6?: string
  ip_updated_at?: string
  last_updated: string
  stale: boolean
  monthly_cost?: number
  balance?: number
  currency?: string
  billing_error?: string
  keep_alive?: boolean | null
  shutdown_mode?: string
  schedule_enabled?: boolean
  schedule_mode?: 'simple' | 'advanced'
  start_time?: string
  stop_time?: string
  daily_report?: boolean | null
  daily_report_time?: string
}

export type StatusResponse = { accounts: AccountSummary[]; system_last_run: string }
export type Job = { id: string; status: 'queued' | 'running' | 'completed' | 'failed'; result?: string; error?: string }
export type History = { hourly: { at: string; traffic: number }[]; daily: { at: string; traffic: number }[] }
export type LogEntry = { id: number; type: string; message: string; created_at: string }
export type APIKeyRecord = { id: number; name: string; scopes: string[]; created_at: string; last_used_at?: string; expires_at?: string; revoked_at?: string }
export type PasskeyRecord = { id: number; name: string; created_at: string; last_used_at?: string }
export type SystemInfo = { version: string; commit: string; built_at: string; repository: string; release_url: string; latest_version?: string; check_error?: string }

export const emptyAccount = (): Account => ({
  id: 0,
  access_key_id: '',
  access_key_secret: '',
  secret_configured: false,
  region_id: 'cn-hongkong',
  instance_id: '',
  max_traffic: 200,
  schedule_enabled: false,
  schedule_mode: 'simple',
  start_time: '08:00',
  stop_time: '23:30',
  remark: '',
  site_type: 'china',
  keep_alive: undefined,
  shutdown_mode: '',
  daily_report: undefined,
  daily_report_time: "00:00",
})

export const defaultConfig = (): Config => ({
  traffic_threshold: 95,
  enable_schedule_notification: false,
  shutdown_mode: 'KeepCharging',
  threshold_action: 'stop_and_notify',
  keep_alive: false,
  api_interval: 600,
  enable_billing: false,
  timezone: 'Asia/Shanghai',
  enable_daily_report: false,
  daily_report_time: '22:00',
  notifications: {
    email: { enabled: false, to: '', host: '', port: 465, username: '', password: '', password_configured: false, security: 'ssl' },
    telegram: { enabled: false, token: '', token_configured: false, chat_id: '', proxy_type: 'none', proxy_url: '', proxy_ip: '', proxy_port: '', proxy_user: '', proxy_pass: '', proxy_password_configured: false },
    webhook: { enabled: false, url: '', method: 'GET', request_type: 'JSON', headers: '', body: '', provider: 'generic', secret: '', secret_configured: false },
  },
  accounts: [],
})
