import { Children, cloneElement, isValidElement, lazy, Suspense, useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import type { CSSProperties, ReactElement } from 'react'
import { createPortal } from 'react-dom'
import {
	  Activity, AlertTriangle, ArrowLeft, ArrowRight, Bell, CalendarClock, Check, ChevronDown, ChevronRight, CircleDollarSign,
	  Clock3, Cloud, Copy, Database, ExternalLink, Eye, EyeOff, FileClock, FileText, Fingerprint, Gauge,
	  Globe2, History as HistoryIcon, Info, KeyRound, LoaderCircle, LockKeyhole, LogOut, MapPin,
  Mail, Menu, Play, Plus, Power, RefreshCw, Save, Search, Server, Settings, ShieldCheck,
  Trash2, UserCog, Webhook, X, Zap,
} from 'lucide-react'
import { APIError, api, fetchLatestReleaseFromGitHub, waitForJob } from './api'
import TimePicker from './TimePicker'
import { ThemeMenu } from './Appearance'
import {
  APIKeyRecord, Account, AccountSummary, Config, History, Job, LogEntry, PasskeyRecord,
  StatusResponse, SystemInfo, defaultConfig, emptyAccount,
} from './types'

type Phase = 'loading' | 'setup' | 'login' | 'dashboard' | 'fatal'
type Toast = { id: number; tone: 'success' | 'error' | 'info'; message: string }
type PasskeyCeremony = { session_id: string; public_key: { publicKey: Record<string, unknown> } }
type SelectOption = { value: string; label: string; meta?: string }
type SelectPosition = { left: number; top: number; width: number; maxHeight: number; placement: 'up' | 'down' }

const HistoryChart = lazy(() => import('./HistoryChart'))

const regions: SelectOption[] = [
  ['cn-hongkong', '中国香港'], ['cn-hangzhou', '华东 1（杭州）'], ['cn-shanghai', '华东 2（上海）'],
  ['cn-qingdao', '华北 1（青岛）'], ['cn-beijing', '华北 2（北京）'], ['cn-zhangjiakou', '华北 3（张家口）'],
  ['cn-huhehaote', '华北 5（呼和浩特）'], ['cn-wulanchabu', '华北 6（乌兰察布）'], ['cn-shenzhen', '华南 1（深圳）'],
  ['cn-heyuan', '华南 2（河源）'], ['cn-guangzhou', '华南 3（广州）'], ['cn-chengdu', '西南 1（成都）'],
  ['ap-southeast-1', '新加坡'], ['ap-northeast-1', '日本（东京）'], ['ap-northeast-2', '韩国（首尔）'], ['us-west-1', '美国（硅谷）'], ['us-east-1', '美国（弗吉尼亚）'],
].map(([value, label]) => ({ value, label, meta: value }))

const refreshIntervals: SelectOption[] = [
  { value: '60', label: '1 分钟' }, { value: '300', label: '5 分钟' }, { value: '600', label: '10 分钟' },
  { value: '1800', label: '30 分钟' }, { value: '3600', label: '1 小时' },
]
const minCustomAPIInterval = 30

export default function App() {
  const [phase, setPhase] = useState<Phase>('loading')
  const [status, setStatus] = useState<StatusResponse>({ accounts: [], system_last_run: '' })
  const [config, setConfig] = useState<Config | null>(null)
  const [fatal, setFatal] = useState('')
  const [toasts, setToasts] = useState<Toast[]>([])
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [adminOpen, setAdminOpen] = useState(false)
  const [historyAccount, setHistoryAccount] = useState<AccountSummary | null>(null)

  const notify = useCallback((message: string, tone: Toast['tone'] = 'info') => {
    const id = Date.now() + Math.random()
    setToasts((current) => [...current, { id, message, tone }])
    window.setTimeout(() => setToasts((current) => current.filter((item) => item.id !== id)), 4000)
  }, [])

  const loadDashboard = useCallback(async () => {
    const [nextStatus, nextConfig] = await Promise.all([
      api<StatusResponse>('/api/v1/status'),
      api<Config>('/api/v1/config'),
    ])
    setStatus(nextStatus)
    setConfig(nextConfig)
    setPhase('dashboard')
  }, [])

  const refreshStatus = useCallback(async (fresh = false) => {
    try {
      setStatus(await api<StatusResponse>('/api/v1/status', fresh ? { cache: 'no-store' } : {}))
    } catch (error) {
      if (error instanceof APIError && error.status === 401) setPhase('login')
    }
  }, [])

  useEffect(() => {
    void (async () => {
      try {
        const init = await api<{ initialized: boolean }>('/api/v1/system/init-status')
        if (!init.initialized) {
          setPhase('setup')
          return
        }
        try {
          await loadDashboard()
        } catch (error) {
          if (error instanceof APIError && error.status === 401) setPhase('login')
          else throw error
        }
      } catch (error) {
        setFatal(error instanceof Error ? error.message : '服务初始化失败')
        setPhase('fatal')
      }
    })()
  }, [loadDashboard])

  useEffect(() => {
    if (phase !== 'dashboard') return
    const timer = window.setInterval(() => void refreshStatus(), 30_000)
    return () => window.clearInterval(timer)
  }, [phase, refreshStatus])

  if (phase === 'loading') return <LoadingScreen />
  if (phase === 'fatal') return <FatalScreen message={fatal} />
  if (phase === 'setup') return <SetupWizard onComplete={loadDashboard} notify={notify} />
  if (phase === 'login') return <Login onComplete={loadDashboard} />

  return (
    <>
      <Dashboard
        status={status}
        config={config!}
        onRefresh={refreshStatus}
        onSettings={() => setSettingsOpen(true)}
        onAdmin={() => setAdminOpen(true)}
        onHistory={setHistoryAccount}
        onConfigChange={setConfig}
        notify={notify}
        onLogout={() => setPhase('login')}
      />
      {settingsOpen && config && (
        <SettingsPanel
          initial={config}
          onClose={() => setSettingsOpen(false)}
          onSaved={(next) => { setConfig(next); setSettingsOpen(false); void refreshStatus(true) }}
          notify={notify}
        />
      )}
      {adminOpen && <AdminSettingsPanel onClose={() => setAdminOpen(false)} notify={notify} />}
      {historyAccount && <HistoryModal account={historyAccount} onClose={() => setHistoryAccount(null)} />}
      <ToastStack items={toasts} />
    </>
  )
}

function LoadingScreen() {
  return (
    <main className="center-stage">
      <div className="brand-lockup brand-lockup--center"><BrandMark /><div><b>CDT MONITOR</b><span>CONTROL PLANE</span></div></div>
      <LoaderCircle className="spin loading-glyph" aria-label="加载中" />
    </main>
  )
}

function FatalScreen({ message }: { message: string }) {
  return (
    <main className="center-stage">
      <section className="glass-card auth-card" role="alert">
        <div className="large-icon large-icon--danger"><AlertTriangle /></div>
        <p className="eyebrow">SERVICE ERROR</p>
        <h1>控制台暂时不可用</h1>
        <p className="muted">{message}</p>
        <button className="button button--primary" onClick={() => location.reload()}><RefreshCw size={18} />重新连接</button>
      </section>
    </main>
  )
}

function SetupWizard({ onComplete, notify }: { onComplete: () => Promise<void>; notify: (message: string, tone?: Toast['tone']) => void }) {
  const [step, setStep] = useState(0)
  const [config, setConfig] = useState<Config>(() => ({ ...defaultConfig(), accounts: [emptyAccount()] }))
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [visible, setVisible] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const strength = Math.min(4, [password.length >= 10, /[A-Z]/.test(password), /[0-9]/.test(password), /[^A-Za-z0-9]/.test(password)].filter(Boolean).length)
  const setupIntervalPreset = refreshIntervals.map((option) => option.value).includes(String(config.api_interval))

  const next = () => {
    setError('')
    if (step === 0 && (password.length < 10 || password !== confirm)) {
      setError(password.length < 10 ? '管理员密码至少需要 10 个字符' : '两次输入的密码不一致')
      return
    }
    setStep((value) => Math.min(2, value + 1))
  }
  const finish = async () => {
    setBusy(true); setError('')
    try {
      const cleaned = structuredClone(config)
      cleaned.admin_password = password
      cleaned.accounts = cleaned.accounts.filter((account) => account.access_key_id.trim())
      await api('/api/v1/setup', { method: 'POST', body: JSON.stringify(cleaned) })
      notify('系统初始化完成', 'success')
      await onComplete()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '初始化失败')
    } finally { setBusy(false) }
  }

  return (
    <main className="setup-shell">
      <header className="setup-header"><div className="brand-lockup"><BrandMark /><div><b>CDT MONITOR</b><span>SECURE SETUP</span></div></div><div className="setup-header-actions"><StepDots current={step} /><ThemeMenu /></div></header>
      <section className="glass-card setup-card">
        <div className="setup-copy">
          <p className="eyebrow">STEP {step + 1} OF 3</p>
          <h1>{['创建安全边界', '设定自动化策略', '连接云端实例'][step]}</h1>
          <p className="muted">{['凭据将使用本机主密钥加密保存。', '默认策略可在控制台中随时调整。', 'AccessKey 仅在服务端解密使用。'][step]}</p>
        </div>

        {step === 0 && (
          <div className="form-stack">
            <PasswordField label="管理员密码" value={password} visible={visible} onVisible={() => setVisible(!visible)} onChange={setPassword} autoComplete="new-password" />
            <div className="strength" aria-label={`密码强度 ${strength}/4`}>{[0, 1, 2, 3].map((index) => <i key={index} className={index < strength ? 'active' : ''} />)}</div>
            <PasswordField label="确认密码" value={confirm} visible={visible} onVisible={() => setVisible(!visible)} onChange={setConfirm} autoComplete="new-password" />
            <div className="security-note"><ShieldCheck size={20} /><span>Argon2id 哈希 · AES-GCM 凭据加密 · SameSite Session</span></div>
          </div>
        )}
        {step === 1 && (
          <div className="form-grid">
            <Field label="流量告警阈值"><input type="number" min={1} max={100} value={config.traffic_threshold} onChange={(event) => setConfig({ ...config, traffic_threshold: Number(event.target.value) })} /><span className="suffix">%</span></Field>
            <SelectField label="状态刷新频率" value={setupIntervalPreset ? `${config.api_interval}` : 'custom'} options={[...refreshIntervals.slice(0, 4), { value: 'custom', label: '自定义' }]} onChange={(value) => setConfig({ ...config, api_interval: value === 'custom' ? minCustomAPIInterval : Number(value) })} />
            {!setupIntervalPreset && <Field label="自定义间隔"><input type="number" min={minCustomAPIInterval} max={86400} value={config.api_interval} onChange={(event) => setConfig({ ...config, api_interval: Math.max(minCustomAPIInterval, Math.min(86400, Number(event.target.value) || minCustomAPIInterval)) })} /><span className="suffix">秒</span></Field>}
            <div className="field field--wide"><label>停机模式</label><Segmented value={config.shutdown_mode} options={[['KeepCharging', '普通停机'], ['StopCharging', '节省停机']]} onChange={(value) => setConfig({ ...config, shutdown_mode: value as Config['shutdown_mode'] })} /></div>
            <ToggleRow title="抢占式实例保活" icon={<Activity />} checked={config.keep_alive} onChange={(checked) => setConfig({ ...config, keep_alive: checked })} />
            <ToggleRow title="账单与余额显示" icon={<CircleDollarSign />} checked={config.enable_billing} onChange={(checked) => setConfig({ ...config, enable_billing: checked })} />
          </div>
        )}
        {step === 2 && (
          <AccountFields account={config.accounts[0]} onChange={(account) => setConfig({ ...config, accounts: [account] })} compact />
        )}

        {error && <div className="inline-error"><AlertTriangle size={16} />{error}</div>}
        <footer className="setup-actions">
          <button className="button button--secondary" onClick={() => setStep((value) => Math.max(0, value - 1))} disabled={step === 0 || busy}><ArrowLeft size={18} />返回</button>
          {step < 2 ? <button className="button button--primary" onClick={next}>继续<ArrowRight size={18} /></button> : <button className="button button--primary" onClick={() => void finish()} disabled={busy}>{busy ? <LoaderCircle className="spin" size={18} /> : <Check size={18} />}完成安装</button>}
        </footer>
      </section>
    </main>
  )
}

function Login({ onComplete }: { onComplete: () => Promise<void> }) {
  const [password, setPassword] = useState('')
  const [visible, setVisible] = useState(false)
  const [busy, setBusy] = useState(false)
  const [passkeyBusy, setPasskeyBusy] = useState(false)
  const [error, setError] = useState('')
  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); setBusy(true); setError('')
    try {
      await api('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ password }) })
      await onComplete()
    } catch (cause) { setError(cause instanceof Error ? cause.message : '登录失败') }
    finally { setBusy(false) }
  }
  const loginWithPasskey = async () => {
    setPasskeyBusy(true); setError('')
    try {
      const begin = await api<PasskeyCeremony>('/api/v1/auth/passkeys/begin', { method: 'POST', body: '{}' })
      const credential = await navigator.credentials.get({ publicKey: decodeRequestOptions(begin.public_key.publicKey) })
      if (!(credential instanceof PublicKeyCredential)) throw new Error('未选择 Passkey')
      await api(`/api/v1/auth/passkeys/complete?session_id=${encodeURIComponent(begin.session_id)}`, { method: 'POST', body: JSON.stringify(serializeCredential(credential)) })
      await onComplete()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Passkey 登录失败') }
    finally { setPasskeyBusy(false) }
  }
  return (
    <main className="login-shell">
      <div className="auth-appearance"><ThemeMenu /></div>
      <div className="login-card-stack">
        <div className="login-brand"><div className="brand-lockup"><BrandMark /><div><b>CDT MONITOR</b><span>CONTROL PLANE</span></div></div><p>阿里云 CDT 流量与实例自动化控制台</p></div>
        <form className="glass-card auth-card" onSubmit={submit}>
          <div className="large-icon"><LockKeyhole /></div>
          <p className="eyebrow">ADMIN ACCESS</p><h1>欢迎回来</h1><p className="muted">使用管理员凭据进入控制台</p>
          <PasswordField label="管理员密码" value={password} visible={visible} onVisible={() => setVisible(!visible)} onChange={setPassword} autoComplete="current-password" />
          {error && <div className="inline-error"><AlertTriangle size={16} />{error}</div>}
          <button className="button button--primary button--full" disabled={busy || !password}>{busy ? <LoaderCircle className="spin" size={18} /> : <ArrowRight size={18} />}安全登录</button>
          {passkeyAvailable() && <button type="button" className="button button--secondary button--full" disabled={passkeyBusy} onClick={() => void loginWithPasskey()}>{passkeyBusy ? <LoaderCircle className="spin" size={18} /> : <Fingerprint size={18} />}使用 Passkey 登录</button>}
        </form>
      </div>
    </main>
  )
}

function Dashboard({ status, config, onRefresh, onSettings, onAdmin, onHistory, onConfigChange, notify, onLogout }: {
  status: StatusResponse; config: Config; onRefresh: (fresh?: boolean) => Promise<void>; onSettings: () => void; onAdmin: () => void; onHistory: (account: AccountSummary) => void; onConfigChange?: (config: Config) => void; notify: (message: string, tone?: Toast['tone']) => void; onLogout: () => void
}) {
  const [configuringAccount, setConfiguringAccount] = useState<Account | null>(null)
	const [ipSelection, setIPSelection] = useState<{ account: AccountSummary; anchor: HTMLElement } | null>(null)
  const [busy, setBusy] = useState<Record<number, string>>({})
  const [mobileMenu, setMobileMenu] = useState(false)
  const [refreshingAll, setRefreshingAll] = useState(false)
  const running = status.accounts.filter((account) => account.instance_status === 'Running').length
  const warning = status.accounts.filter((account) => account.over_threshold).length
  const used = status.accounts.reduce((sum, account) => sum + account.flow_used, 0)
  const heartbeatAge = status.system_last_run ? Math.floor((Date.now() - new Date(status.system_last_run).getTime()) / 1000) : Infinity

  const openInstanceSettings = (accountSummary: AccountSummary) => {
    const found = config.accounts.find((a) => a.id === accountSummary.id)
    if (found) {
      setConfiguringAccount({
        ...found,
        keep_alive: accountSummary.keep_alive ?? found.keep_alive,
        shutdown_mode: (accountSummary.shutdown_mode as Account['shutdown_mode']) ?? found.shutdown_mode ?? '',
        schedule_enabled: accountSummary.schedule_enabled ?? found.schedule_enabled,
			schedule_mode: accountSummary.schedule_mode ?? found.schedule_mode ?? ((found.start_cron || found.stop_cron) ? 'advanced' : 'simple'),
        start_time: accountSummary.start_time ?? found.start_time,
        stop_time: accountSummary.stop_time ?? found.stop_time,
        daily_report: accountSummary.daily_report ?? found.daily_report,
        daily_report_time: accountSummary.daily_report_time ?? found.daily_report_time ?? '00:00',
      })
    } else {
      setConfiguringAccount({
        id: accountSummary.id,
        access_key_id: '',
        secret_configured: true,
        region_id: accountSummary.region,
        instance_id: '',
        max_traffic: accountSummary.flow_total,
        schedule_enabled: accountSummary.schedule_enabled || false,
			schedule_mode: accountSummary.schedule_mode || 'simple',
        start_time: accountSummary.start_time || '08:00',
        stop_time: accountSummary.stop_time || '23:30',
        remark: accountSummary.remark,
        site_type: 'china',
        keep_alive: accountSummary.keep_alive,
        shutdown_mode: (accountSummary.shutdown_mode as Account['shutdown_mode']) || '',
        daily_report: accountSummary.daily_report,
        daily_report_time: accountSummary.daily_report_time || '00:00',
      })
    }
  }

  const runAction = async (account: AccountSummary, action: 'start' | 'stop' | 'refresh') => {
    const effectiveKeepAlive = account.keep_alive != null ? account.keep_alive : config.keep_alive
    if (action === 'stop' && effectiveKeepAlive) { notify('保活启用时不能手动关机', 'error'); return }
    setBusy((value) => ({ ...value, [account.id]: action }))
    try {
      const path = action === 'refresh' ? `/api/v1/accounts/${account.id}/refresh` : `/api/v1/accounts/${account.id}/actions/${action}`
      const job = await api<Job>(path, { method: 'POST', body: '{}' })
      await waitForJob(job.id)
      await onRefresh(true)
      notify(action === 'refresh' ? '实例状态已刷新' : `已发送${action === 'start' ? '开机' : '关机'}指令`, 'success')
    } catch (error) { notify(error instanceof Error ? error.message : '操作失败', 'error') }
    finally { setBusy((value) => { const next = { ...value }; delete next[account.id]; return next }) }
  }
  const logout = async () => {
    await api('/api/v1/auth/logout', { method: 'POST', body: '{}' }).catch(() => undefined)
    onLogout()
  }
  const refreshAll = async () => {
    if (refreshingAll) return
    setRefreshingAll(true)
    try {
      const result = await api<{ jobs?: Job[] }>('/api/v1/accounts/refresh', { method: 'POST', body: '{}' })
      const jobs = Array.isArray(result.jobs) ? result.jobs : []
      if (jobs.length === 0) {
        await onRefresh(true)
        notify('暂无可刷新的实例', 'info')
        return
      }
      const outcomes = await Promise.allSettled(jobs.map((job) => waitForJob(job.id)))
      await onRefresh(true)
      const succeeded = outcomes.filter((outcome) => outcome.status === 'fulfilled').length
      if (succeeded === jobs.length) notify(`已强制刷新 ${succeeded} 个实例`, 'success')
      else if (succeeded > 0) notify(`已刷新 ${succeeded}/${jobs.length} 个实例，其余实例刷新失败`, 'error')
      else notify('全部实例刷新失败，请查看运行日志', 'error')
    } catch (error) {
      notify(error instanceof Error ? error.message : '全部实例刷新失败', 'error')
    } finally {
      setRefreshingAll(false)
    }
  }
  const openSettings = () => { setMobileMenu(false); onSettings() }
  const openAdmin = () => { setMobileMenu(false); onAdmin() }
  const leave = () => { setMobileMenu(false); void logout() }

  return (
	    <main className="app-shell">
      <header className="topbar">
        <div className="brand-lockup"><BrandMark /><div><b>CDT MONITOR</b><span>CONTROL PLANE</span></div></div>
        <div id="dashboard-actions" className={`topbar-actions ${mobileMenu ? 'open' : ''}`} aria-busy={refreshingAll}>
          <div className={`heartbeat ${heartbeatAge > 180 ? 'heartbeat--warn' : ''}`}><i />{heartbeatAge > 180 ? '监控任务延迟' : '自动化运行中'}</div>
          <IconButton label={refreshingAll ? '正在强制刷新全部实例' : '强制刷新全部实例'} disabled={refreshingAll} onClick={() => void refreshAll()}>{refreshingAll ? <LoaderCircle className="spin" size={18} /> : <RefreshCw size={18} />}</IconButton>
          <ThemeMenu />
          <IconButton label="设置" onClick={openSettings}><Settings size={18} /></IconButton>
          <IconButton label="管理员" title="管理员设置" onClick={openAdmin}><UserCog size={18} /></IconButton>
          <IconButton label="退出" onClick={leave}><LogOut size={18} /></IconButton>
        </div>
        <IconButton label="菜单" className="mobile-menu" ariaExpanded={mobileMenu} ariaControls="dashboard-actions" onClick={() => setMobileMenu(!mobileMenu)}>{mobileMenu ? <X /> : <Menu />}</IconButton>
      </header>

      <section className="overview-head"><div><p className="eyebrow">LIVE INFRASTRUCTURE</p><h1>资源控制台</h1><p className="muted">{new Date().toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' })}</p></div><div className="overview-time"><Clock3 size={17} /><span>上次任务</span><b>{status.system_last_run ? formatTime(status.system_last_run) : '尚未运行'}</b></div></section>
      <section className="metric-strip">
        <Metric icon={<Server />} label="实例总数" value={`${status.accounts.length}`} suffix="台" tone="blue" />
        <Metric icon={<Activity />} label="运行中" value={`${running}`} suffix="台" tone="green" />
        <Metric icon={<Gauge />} label="累计流量" value={used.toFixed(1)} suffix="GB" tone="cyan" />
        <Metric icon={<AlertTriangle />} label="阈值告警" value={`${warning}`} suffix="项" tone="amber" />
      </section>

      <div className="section-heading"><div><p className="eyebrow">MANAGED INSTANCES</p><h2>实例与流量</h2></div><span>{status.accounts.length} 个配置</span></div>
      {status.accounts.length === 0 ? (
        <button className="empty-state" onClick={onSettings}><Cloud /><h3>添加第一个云端实例</h3><p>进入设置完成 AccessKey 与实例信息配置</p><span>打开设置<ChevronRight size={16} /></span></button>
      ) : (
        <section className="account-grid">
			          {status.accounts.map((account) => <AccountCard key={account.id} account={account} busy={refreshingAll ? 'refresh' : busy[account.id]} keepAlive={config.keep_alive} billingEnabled={config.enable_billing} onAction={(action) => void runAction(account, action)} onConfigure={() => openInstanceSettings(account)} onHistory={() => onHistory(account)} onIP={(anchor) => setIPSelection({ account, anchor })} />)}
        </section>
      )}
      {configuringAccount && (
        <InstanceSettingsModal
          account={configuringAccount}
	          timezone={config.timezone}
          onClose={() => setConfiguringAccount(null)}
          onSaved={(updated) => {
            const nextAccounts = config.accounts.map((a) => (a.id === updated.id ? { ...a, ...updated } : a))
            if (onConfigChange) onConfigChange({ ...config, accounts: nextAccounts })
            setConfiguringAccount(null)
            void onRefresh(true)
          }}
          notify={notify}
        />
      )}
			      {ipSelection && <PublicIPModal account={ipSelection.account} anchor={ipSelection.anchor} instanceID={config.accounts.find((item) => item.id === ipSelection.account.id)?.instance_id || ''} onClose={() => setIPSelection(null)} />}
    </main>
  )
}

function AccountCard({ account, busy, keepAlive, billingEnabled, onAction, onConfigure, onHistory, onIP }: { account: AccountSummary; busy?: string; keepAlive: boolean; billingEnabled: boolean; onAction: (action: 'start' | 'stop' | 'refresh') => void; onConfigure: () => void; onHistory: () => void; onIP: (anchor: HTMLElement) => void }) {
  const statusTone = statusClass(account.instance_status)
  const currency = account.currency === 'USD' ? '$' : '¥'
  const hasBilling = account.monthly_cost !== undefined || account.balance !== undefined
  const effectiveKeepAlive = account.keep_alive != null ? account.keep_alive : keepAlive
  return (
    <article className={`glass-card account-card ${account.over_threshold ? 'account-card--alert' : ''}`}>
	      <header className="account-card__header">
        <div className={`status-icon ${statusTone}`}><Server size={20} /></div>
	        <div className="account-title"><h3>{account.remark || account.account}</h3><div className="account-title__meta"><MapPin size={13} aria-hidden="true" /><span>{account.region_name}</span><button type="button" className="account-ip-button" onClick={(event) => { const anchor = event.currentTarget.closest('.account-card') as HTMLElement | null; if (anchor) onIP(anchor) }} aria-label="查看公网 IP"><Globe2 size={13} />IP</button></div></div>
        <div className="account-card__side">
          <div className={`status-pill ${statusTone}`}><i />{statusLabel(account.instance_status)}</div>
          {billingEnabled && <div className="account-billing" aria-label="账单与余额"><div><span>本月费用</span><b>{account.monthly_cost === undefined ? '待同步' : `${currency}${account.monthly_cost.toFixed(2)}`}</b></div><div><span>账户余额</span><b>{account.balance === undefined ? '待同步' : `${currency}${account.balance.toFixed(2)}`}</b></div><small className={account.billing_error ? 'billing-error' : ''}>{account.billing_error || (hasBilling ? '已同步' : '待同步')}</small></div>}
        </div>
      </header>
      <div className="traffic-value"><div><span>本月 CDT 流量</span><strong>{account.flow_used.toFixed(2)}</strong><small> / {account.flow_total.toFixed(0)} GB</small></div><button className="mini-icon" onClick={onHistory} aria-label="查看历史流量"><HistoryIcon size={17} /></button></div>
      <div className="progress-track"><i style={{ width: `${Math.min(100, account.percentage)}%` }} className={account.over_threshold ? 'danger' : account.percentage >= account.threshold * .8 ? 'warning' : ''} /></div>
      <div className="progress-meta"><span>{account.percentage.toFixed(2)}% 已使用</span><span>阈值 {account.threshold}%</span></div>
      <footer className="account-card__footer">
        <span className={account.stale ? 'stale' : ''}><Clock3 size={14} />{account.last_updated ? formatTime(account.last_updated) : '等待首次同步'}</span>
        <div className="control-group">
          <IconButton label="实例设置" onClick={onConfigure}><Settings /></IconButton>
          <IconButton label="刷新实例" disabled={!!busy} onClick={() => onAction('refresh')}>{busy === 'refresh' ? <LoaderCircle className="spin" /> : <RefreshCw />}</IconButton>
          {account.instance_status === 'Stopped' && <IconButton label="开机" disabled={!!busy} tone="positive" onClick={() => onAction('start')}>{busy === 'start' ? <LoaderCircle className="spin" /> : <Play />}</IconButton>}
          {account.instance_status === 'Running' && <IconButton label={effectiveKeepAlive ? '保活启用，不能关机' : '关机'} disabled={!!busy || effectiveKeepAlive} tone="danger" onClick={() => onAction('stop')}>{busy === 'stop' ? <LoaderCircle className="spin" /> : <Power />}</IconButton>}
        </div>
      </footer>
    </article>
  )
}

function PublicIPModal({ account, anchor, instanceID, onClose }: { account: AccountSummary; anchor: HTMLElement; instanceID: string; onClose: () => void }) {
  const panelRef = useRef<HTMLElement>(null)
  const [position, setPosition] = useState({ left: 0, top: 0 })
  const addresses = [account.public_ip, account.public_ipv6].filter((value): value is string => Boolean(value))
  const copyIP = (value: string) => {
    if (value) void navigator.clipboard?.writeText(value)
  }
  useLayoutEffect(() => {
    const updatePosition = () => {
      const panel = panelRef.current
      const rect = anchor.getBoundingClientRect()
	      const width = Math.min(360, window.innerWidth - 28)
      const height = panel?.offsetHeight || 320
      const isMobile = window.matchMedia('(max-width: 640px)').matches
      const centerX = isMobile ? window.innerWidth / 2 : rect.left + rect.width / 2
      const centerY = isMobile ? window.innerHeight / 2 : rect.top + rect.height / 2
      setPosition({
	        left: Math.max(width / 2 + 14, Math.min(window.innerWidth - width / 2 - 14, centerX)),
        top: Math.max(height / 2 + 12, Math.min(window.innerHeight - height / 2 - 12, centerY)),
      })
    }
    updatePosition()
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
    return () => {
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
    }
  }, [anchor])
  return <div className="modal-layer modal-layer--nested modal-layer--public-ip" role="dialog" aria-modal="true" aria-label="公网 IP 信息" style={{ '--ip-modal-x': `${position.left}px`, '--ip-modal-y': `${position.top}px` } as CSSProperties}>
    <div className="modal-scrim" onClick={onClose} />
    <section ref={panelRef} className="glass-panel public-ip-modal">
      <header><div><p className="eyebrow">NETWORK IDENTITY</p><h2>公网 IP</h2><p className="public-ip-context">{account.remark || account.account}<span aria-hidden="true">·</span>{account.region_name}</p></div><IconButton label="关闭" onClick={onClose}><X /></IconButton></header>
      <div className="public-ip-modal__body">
        <div className="public-ip-addresses">
          {addresses.length ? addresses.map((value) => { const version = value === account.public_ipv6 ? 'IPv6' : 'IPv4'; return <div className="public-ip-value" key={value}><div className="public-ip-value__copy"><span className="public-ip-value__type">{version}</span><strong>{value}</strong></div><IconButton className="public-ip-copy-button" label={`复制 ${version} 地址`} onClick={() => copyIP(value)}><Copy /></IconButton></div> }) : <div className="public-ip-empty"><Globe2 size={18} /><span>{account.ip_updated_at ? '未分配公网地址' : '等待首次同步'}</span></div>}
        </div>
        {instanceID && <div className="public-ip-instance"><span>实例 ID</span><code>{instanceID}</code></div>}
      </div>
    </section>
  </div>
}

function InstanceSettingsModal({
  account,
  timezone,
  onClose,
  onSaved,
  notify,
}: {
  account: Account
  timezone: string
  onClose: () => void
  onSaved: (account: Account) => void
  notify: (message: string, tone?: Toast['tone']) => void
}) {
  const [busy, setBusy] = useState(false)
  const [form, setForm] = useState<{
    keep_alive?: boolean | null
    shutdown_mode: '' | 'KeepCharging' | 'StopCharging'
    schedule_enabled: boolean
	    schedule_mode: 'simple' | 'advanced'
    start_time: string
    stop_time: string
    daily_report?: boolean | null
    daily_report_time: string
    start_cron: string
    stop_cron: string
  }>({
    keep_alive: account.keep_alive,
    shutdown_mode: (account.shutdown_mode as '' | 'KeepCharging' | 'StopCharging') || '',
    schedule_enabled: account.schedule_enabled,
	    schedule_mode: account.schedule_mode || ((account.start_cron || account.stop_cron) ? 'advanced' : 'simple'),
    start_time: account.start_time || '08:00',
    stop_time: account.stop_time || '23:30',
    daily_report: account.daily_report,
    daily_report_time: account.daily_report_time || '00:00',
    start_cron: account.start_cron || '',
    stop_cron: account.stop_cron || '',
  })

  const save = async () => {
    setBusy(true)
    try {
      const payload = {
        keep_alive: form.keep_alive,
        shutdown_mode: form.shutdown_mode || 'default',
        schedule_enabled: form.schedule_enabled,
	        schedule_mode: form.schedule_mode,
        start_time: form.start_time,
        stop_time: form.stop_time,
        daily_report: form.daily_report,
        daily_report_time: form.daily_report_time || '00:00',
        start_cron: form.start_cron,
        stop_cron: form.stop_cron,
      }
      const updated = await api<Account>(`/api/v1/accounts/${account.id}/settings`, {
        method: 'PATCH',
        body: JSON.stringify(payload),
      })
      notify('实例配置已保存', 'success')
      onSaved(updated)
    } catch (error) {
      notify(error instanceof Error ? error.message : '保存失败', 'error')
    } finally {
      setBusy(false)
    }
  }

  const keepAliveVal = form.keep_alive == null ? 'default' : form.keep_alive ? 'enabled' : 'disabled'
  const shutdownVal = !form.shutdown_mode ? 'default' : form.shutdown_mode
  const dailyReportVal = form.daily_report == null ? 'default' : form.daily_report ? 'enabled' : 'disabled'

  return (
    <div className="modal-layer" role="dialog" aria-modal="true" aria-label="实例配置">
      <div className="modal-scrim" onClick={onClose} />
      <section className="glass-panel instance-settings-modal">
        <header>
          <div>
            <p className="eyebrow">INSTANCE PREFERENCES</p>
            <h2>{account.remark || account.instance_id || '实例配置'}</h2>
          </div>
          <IconButton label="关闭" onClick={onClose}><X /></IconButton>
        </header>
        <div className="instance-settings-modal__body">
          <div className="field">
            <label>抢占式保活</label>
            <Segmented
              value={keepAliveVal}
              options={[
                ['default', '跟随全局'],
                ['enabled', '单独开启'],
                ['disabled', '单独关闭'],
              ]}
              onChange={(val) => setForm({ ...form, keep_alive: val === 'default' ? null : val === 'enabled' })}
            />
          </div>
          <div className="field">
            <label>默认停机方式</label>
            <Segmented
              value={shutdownVal}
              options={[
                ['default', '跟随全局'],
                ['KeepCharging', '普通停机'],
                ['StopCharging', '节省停机'],
              ]}
              onChange={(val) => setForm({ ...form, shutdown_mode: val === 'default' ? '' : (val as '' | 'KeepCharging' | 'StopCharging') })}
            />
          </div>
	      <ScheduleOrchestrator enabled={form.schedule_enabled} mode={form.schedule_mode} startTime={form.start_time} stopTime={form.stop_time} startCron={form.start_cron} stopCron={form.stop_cron} timezone={timezone} onChange={(patch) => setForm((current) => ({ ...current, ...patch }))} />
          <div className="field">
            <label>参与日报推送</label>
            <Segmented
              value={dailyReportVal}
              options={[
                ['default', '跟随全局'],
                ['enabled', '单独开启'],
                ['disabled', '单独关闭'],
              ]}
              onChange={(val) => setForm({ ...form, daily_report: val === 'default' ? null : val === 'enabled' })}
            />
	          {form.schedule_enabled && form.schedule_mode === 'simple' && <ScheduleReportNote stopTime={form.stop_time} />}
          </div>
	      {(!form.schedule_enabled || form.schedule_mode === 'advanced') && (
            <div>
              <TimePicker label="日报推送时间" value={form.daily_report_time || '00:00'} onChange={(value) => setForm({ ...form, daily_report_time: value })} />
              <DailyReportNote time={form.daily_report_time} />
            </div>
          )}
        </div>
        <footer>
          <button className="button button--secondary" onClick={onClose} disabled={busy}>取消</button>
          <button className="button button--primary" onClick={() => void save()} disabled={busy}>
            {busy ? <LoaderCircle className="spin" /> : <Save />}保存配置
          </button>
        </footer>
      </section>
    </div>
  )
}

function SettingsPanel({ initial, onClose, onSaved, notify }: { initial: Config; onClose: () => void; onSaved: (config: Config) => void; notify: (message: string, tone?: Toast['tone']) => void }) {
  const [tab, setTab] = useState<'general' | 'accounts' | 'notify' | 'keys' | 'logs' | 'about'>('general')
  const [config, setConfig] = useState<Config>(() => structuredClone(initial))
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try {
      await api('/api/v1/config', { method: 'PUT', body: JSON.stringify(config) })
      if (config.enable_billing && !initial.enable_billing) {
        const refreshes = config.accounts.filter((account) => account.id > 0).map((account) => api(`/api/v1/accounts/${account.id}/refresh`, { method: 'POST', body: '{}' }))
        const results = await Promise.allSettled(refreshes)
        if (results.some((result) => result.status === 'fulfilled')) notify('配置已保存，账单同步已开始', 'success')
        else notify('配置已保存，账单将在下次同步时更新', 'info')
      } else {
        notify('配置已安全保存', 'success')
      }
      onSaved(config)
    } catch (error) { notify(error instanceof Error ? error.message : '保存失败', 'error') }
    finally { setBusy(false) }
  }
  const tabs = [
    ['general', <Settings />, '系统'], ['accounts', <Server />, '实例'], ['notify', <Bell />, '通知'], ['keys', <KeyRound />, 'API Key'], ['logs', <FileClock />, '日志'], ['about', <Info />, '关于'],
  ] as const
  return (
    <div className="modal-layer" role="dialog" aria-modal="true" aria-label="系统设置">
      <div className="modal-scrim" onClick={onClose} />
      <section className="settings-panel">
        <header><div><p className="eyebrow">SYSTEM PREFERENCES</p><h2>控制台设置</h2></div><IconButton label="关闭" onClick={onClose}><X /></IconButton></header>
        <nav className="settings-tabs">{tabs.map(([value, icon, label]) => <button key={value} className={tab === value ? 'active' : ''} onClick={() => setTab(value)}>{icon}{label}</button>)}</nav>
        <div className="settings-content">
          {tab === 'general' && <GeneralSettings config={config} onChange={setConfig} />}
          {tab === 'accounts' && <AccountSettings config={config} onChange={setConfig} />}
          {tab === 'notify' && <NotificationSettings config={config} onChange={setConfig} notify={notify} />}
          {tab === 'keys' && <APIKeySettings notify={notify} />}
          {tab === 'logs' && <LogSettings notify={notify} />}
          {tab === 'about' && <AboutSettings notify={notify} />}
        </div>
        {(tab === 'general' || tab === 'accounts' || tab === 'notify') && <footer className="settings-footer"><button className="button button--primary" onClick={() => void save()} disabled={busy}>{busy ? <LoaderCircle className="spin" /> : <Save />}保存更改</button></footer>}
      </section>
    </div>
  )
}

function GeneralSettings({ config, onChange }: { config: Config; onChange: (config: Config) => void }) {
  const presetIntervals = refreshIntervals.map((option) => option.value)
  const intervalValue = presetIntervals.includes(String(config.api_interval)) ? String(config.api_interval) : 'custom'
  const updateInterval = (value: string) => {
    onChange({ ...config, api_interval: value === 'custom' ? minCustomAPIInterval : Number(value) })
  }
  return <div className="settings-section"><SectionTitle icon={<Gauge />} title="监控策略" subtitle="AUTOMATION POLICY" />
    <div className="form-grid settings-form">
      <Field label="告警阈值"><input type="number" min={1} max={100} value={config.traffic_threshold} onChange={(event) => onChange({ ...config, traffic_threshold: Number(event.target.value) })} /><span className="suffix">%</span></Field>
      <SelectField label="API 刷新间隔" value={intervalValue} options={[...refreshIntervals, { value: 'custom', label: '自定义' }]} onChange={updateInterval} />
      {intervalValue === 'custom' && <Field label="自定义间隔"><input type="number" min={minCustomAPIInterval} max={86400} step={1} value={config.api_interval} onChange={(event) => onChange({ ...config, api_interval: Math.max(minCustomAPIInterval, Math.min(86400, Number(event.target.value) || minCustomAPIInterval)) })} /><span className="suffix">秒</span></Field>}
      <Field label="系统时区"><input value={config.timezone} onChange={(event) => onChange({ ...config, timezone: event.target.value })} /></Field>
      <div className="field"><label>阈值动作</label><Segmented value={config.threshold_action} options={[['stop_and_notify', '停机并通知'], ['notify_only', '仅通知']]} onChange={(value) => onChange({ ...config, threshold_action: value as Config['threshold_action'] })} /></div>
      <div className="field field--wide"><label>停机模式</label><Segmented value={config.shutdown_mode} options={[['KeepCharging', '普通停机'], ['StopCharging', '节省停机']]} onChange={(value) => onChange({ ...config, shutdown_mode: value as Config['shutdown_mode'] })} /></div>
    </div>
    <div className="toggle-list">
      <ToggleRow title="抢占式实例保活" icon={<Activity />} checked={config.keep_alive} onChange={(checked) => onChange({ ...config, keep_alive: checked })} />
      <ToggleRow title="定时任务通知" icon={<Bell />} checked={config.enable_schedule_notification} onChange={(checked) => onChange({ ...config, enable_schedule_notification: checked })} />
      <ToggleRow title="每日消费与流量日报" icon={<FileText />} checked={config.enable_daily_report} onChange={(checked) => onChange({ ...config, enable_daily_report: checked })} />
      <ToggleRow title="账单与余额" icon={<CircleDollarSign />} checked={config.enable_billing} onChange={(checked) => onChange({ ...config, enable_billing: checked })} />
    </div>
  </div>
}

function AccountSettings({ config, onChange }: { config: Config; onChange: (config: Config) => void }) {
  const update = (index: number, account: Account) => { const accounts = [...config.accounts]; accounts[index] = account; onChange({ ...config, accounts }) }
  const remove = (index: number) => onChange({ ...config, accounts: config.accounts.filter((_, current) => current !== index) })
  const clone = (index: number) => {
    const source = config.accounts[index]
    const copy: Account = {
      ...source,
      id: 0,
      instance_id: '',
      remark: source.remark ? `${source.remark} (副本)` : '',
      secret_configured: source.secret_configured || Boolean(source.access_key_secret),
      access_key_secret: source.access_key_secret || '',
      daily_report_time: source.daily_report_time || '00:00',
    }
    const accounts = [...config.accounts]
    accounts.splice(index + 1, 0, copy)
    onChange({ ...config, accounts })
  }
  return <div className="settings-section"><div className="section-title-row"><SectionTitle icon={<Server />} title="云端实例" subtitle="ALIYUN ACCOUNTS" /><button className="button button--secondary button--small" onClick={() => onChange({ ...config, accounts: [...config.accounts, emptyAccount()] })}><Plus />添加实例</button></div>
	    <div className="account-settings-list">{config.accounts.length === 0 && <div className="subtle-empty"><Database />尚未配置实例</div>}{config.accounts.map((account, index) => <div className="account-editor" key={account.id || `new-${index}`}><div className="account-editor__head"><span>{account.remark || `实例 ${index + 1}`}</span><div className="account-editor__actions"><IconButton label="复制" onClick={() => clone(index)}><Copy /></IconButton><IconButton label="删除" tone="danger" onClick={() => remove(index)}><Trash2 /></IconButton></div></div><AccountFields account={account} timezone={config.timezone} onChange={(next) => update(index, next)} /></div>)}</div>
  </div>
}

function AccountFields({ account, timezone = 'Asia/Shanghai', onChange, compact = false }: { account: Account; timezone?: string; onChange: (account: Account) => void; compact?: boolean }) {
  return <div className={`form-grid account-fields ${compact ? 'compact' : ''}`}>
    <Field label="AccessKey ID"><input autoComplete="off" value={account.access_key_id} onChange={(event) => onChange({ ...account, access_key_id: event.target.value })} placeholder="LTAI5t…" /></Field>
    <Field label={`AccessKey Secret${account.secret_configured ? ' · 已配置' : ''}`}><input type="password" autoComplete="new-password" value={account.access_key_secret || ''} onChange={(event) => onChange({ ...account, access_key_secret: event.target.value })} placeholder={account.secret_configured ? '留空保持不变' : '输入 Secret'} /></Field>
    <Field label="实例 ID"><input value={account.instance_id} onChange={(event) => onChange({ ...account, instance_id: event.target.value })} placeholder="i-bp…" /></Field>
    <SelectField label="地域" value={account.region_id} options={regions} searchable searchPlaceholder="搜索地域名称或代码" onChange={(value) => onChange({ ...account, region_id: value })} />
    <Field label="流量额度"><input type="number" min={1} value={account.max_traffic} onChange={(event) => onChange({ ...account, max_traffic: Number(event.target.value) })} /><span className="suffix">GB</span></Field>
    <SelectField label="站点类型" value={account.site_type} options={[{ value: 'china', label: '中国站', meta: 'CNY' }, { value: 'international', label: '国际站', meta: 'USD' }]} onChange={(value) => onChange({ ...account, site_type: value as Account['site_type'] })} />
    <Field label="备注"><input value={account.remark} onChange={(event) => onChange({ ...account, remark: event.target.value })} placeholder="香港主节点" /></Field>
	    {compact ? <>
	      <ToggleRow title="每日定时开关机" icon={<Clock3 />} checked={account.schedule_enabled} onChange={(checked) => onChange({ ...account, schedule_enabled: checked })} />
	      {account.schedule_enabled && <><TimePicker label="开机时间" value={account.start_time} onChange={(value) => onChange({ ...account, start_time: value })} /><TimePicker label="关机时间" value={account.stop_time} onChange={(value) => onChange({ ...account, stop_time: value })} /></>}
	    </> : <div className="account-fields__full"><ScheduleOrchestrator enabled={account.schedule_enabled} mode={account.schedule_mode || ((account.start_cron || account.stop_cron) ? 'advanced' : 'simple')} startTime={account.start_time} stopTime={account.stop_time} startCron={account.start_cron || ''} stopCron={account.stop_cron || ''} timezone={timezone} onChange={(patch) => onChange({ ...account, ...patch })} />
        <div className="field">
          <label>参与日报推送</label>
          <Segmented
            value={account.daily_report == null ? 'default' : account.daily_report ? 'enabled' : 'disabled'}
            options={[
              ['default', '跟随全局'],
              ['enabled', '单独开启'],
              ['disabled', '单独关闭'],
            ]}
            onChange={(val) => onChange({
              ...account,
              daily_report: val === 'default' ? null : val === 'enabled',
            })}
          />
          {account.schedule_enabled && account.schedule_mode !== 'advanced' && <ScheduleReportNote stopTime={account.stop_time} />}
        </div>
	        {(!account.schedule_enabled || account.schedule_mode === 'advanced') && (
          <div>
            <TimePicker label="日报推送时间" value={account.daily_report_time || '00:00'} onChange={(value) => onChange({ ...account, daily_report_time: value })} />
            <DailyReportNote time={account.daily_report_time} />
          </div>
        )}
      </div>
	    }
  </div>
}

function ScheduleOrchestrator({ enabled, mode, startTime, stopTime, startCron, stopCron, timezone, onChange }: { enabled: boolean; mode: 'simple' | 'advanced'; startTime: string; stopTime: string; startCron: string; stopCron: string; timezone: string; onChange: (patch: Partial<Pick<Account, 'schedule_enabled' | 'schedule_mode' | 'start_time' | 'stop_time' | 'start_cron' | 'stop_cron'>>) => void }) {
  return <section className="schedule-orchestrator" aria-label="定时任务编排">
    <header className="schedule-orchestrator__head"><div className="schedule-orchestrator__title"><span><CalendarClock size={17} /></span><div><h3>定时任务编排</h3><p>{mode === 'simple' ? '按每日时间开机与关机' : '按 Cron 表达式执行开机与关机'}</p></div></div><label className="schedule-enabled"><span>启用</span><input type="checkbox" checked={enabled} onChange={(event) => onChange({ schedule_enabled: event.target.checked })} /><i><em /></i></label></header>
    <div className="schedule-orchestrator__mode" role="group" aria-label="定时任务模式" data-mode={mode}><span className="schedule-orchestrator__thumb" aria-hidden="true" /><button type="button" aria-pressed={mode === 'simple'} onClick={() => onChange({ schedule_mode: 'simple' })}>简单</button><button type="button" aria-pressed={mode === 'advanced'} onClick={() => onChange({ schedule_mode: 'advanced' })}>进阶</button></div>
    {enabled && (mode === 'simple' ? <div className="schedule-time-grid"><TimePicker label="开机时间" value={startTime} onChange={(value) => onChange({ start_time: value })} /><TimePicker label="关机时间" value={stopTime} onChange={(value) => onChange({ stop_time: value })} /></div> : <div className="cron-schedules"><CronScheduleEditor label="定时开机" value={startCron} timezone={timezone} defaultCron="0 0 1 * *" onChange={(value) => onChange({ start_cron: value })} /><CronScheduleEditor label="定时关机" value={stopCron} timezone={timezone} defaultCron="0 0 15 * *" onChange={(value) => onChange({ stop_cron: value })} /></div>)}
  </section>
}

function CronScheduleEditor({ label, value, timezone, defaultCron, onChange }: { label: string; value: string; timezone: string; defaultCron: string; onChange: (value: string) => void }) {
  const selectId = useId()
  const daySelectId = useId()
  const [preview, setPreview] = useState<string[]>([])
  const [previewError, setPreviewError] = useState('')
  const [previewBusy, setPreviewBusy] = useState(false)
  const [mode, setMode] = useState<'off' | 'daily' | 'weekly' | 'monthly' | 'custom'>(() => cronMode(value))
  useEffect(() => setMode(cronMode(value)), [value])
  const parts = (value || defaultCron).trim().split(/\s+/)
  const minute = /^\d+$/.test(parts[0] || '') ? Number(parts[0]) : 0
  const hour = /^\d+$/.test(parts[1] || '') ? Number(parts[1]) : 0
  const time = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
  const day = parts[2] !== '*' && /^\d+$/.test(parts[2] || '') ? parts[2] : (defaultCron.split(/\s+/)[2] === '*' ? '1' : defaultCron.split(/\s+/)[2])
  const weekday = parts[4] !== '*' && /^\d$/.test(parts[4] || '') ? parts[4] : '1'
  const updateMode = (next: 'off' | 'daily' | 'weekly' | 'monthly' | 'custom') => {
    setMode(next)
    if (next === 'off') onChange('')
    else if (next !== 'custom') onChange(buildCron(next, time, day, weekday))
    else if (!value) onChange(defaultCron)
  }
  const updateTime = (next: string) => {
    const [nextHour, nextMinute] = next.split(':')
    onChange(buildCron(mode, next, day, weekday, Number(nextMinute), Number(nextHour)))
  }
  const updateDay = (next: string) => onChange(buildCron(mode, time, next, weekday))
  const updateWeekday = (next: string) => onChange(buildCron(mode, time, day, next))
  const weekdayOptions = ['1', '2', '3', '4', '5', '6', '0'].map((value) => ({
    value,
    label: value === '0' ? '周日' : `周${['一', '二', '三', '四', '五', '六'][Number(value) - 1]}`,
  }))
  const previewPlan = async () => {
    if (!value.trim()) {
      setPreview([])
      setPreviewError('先选择或输入一个表达式')
      return
    }
    setPreviewBusy(true)
    setPreviewError('')
    try {
      const result = await api<{ occurrences: string[] }>('/api/v1/schedules/preview', { method: 'POST', body: JSON.stringify({ expression: value, timezone, count: 5 }) })
      setPreview(result.occurrences.map((item) => formatScheduleTime(item, timezone)))
    } catch (error) {
      setPreview([])
      setPreviewError(error instanceof Error ? error.message : '表达式无法预览')
    } finally {
      setPreviewBusy(false)
    }
  }
  return <div className="cron-editor">
    <div className="cron-editor__head"><div><label>{label}</label><span>五字段：分 时 日 月 周</span></div><button type="button" className="button button--secondary button--small cron-preview-button" disabled={previewBusy || !value} onClick={() => void previewPlan()}>{previewBusy ? <LoaderCircle className="spin" /> : <CalendarClock />}预览</button></div>
    <div className="cron-editor__row">
      <div className="cron-select"><ElegantSelect id={selectId} value={mode} options={[{ value: 'off', label: '不启用' }, { value: 'daily', label: '每天' }, { value: 'weekly', label: '每周' }, { value: 'monthly', label: '每月' }, { value: 'custom', label: '自定义' }]} onChange={(next) => updateMode(next as typeof mode)} searchable={false} searchPlaceholder="" /></div>
      {mode === 'custom' ? <input className="cron-expression" aria-label={`${label}表达式`} value={value} placeholder="0 0 1 * *" onChange={(event) => onChange(event.target.value)} /> : mode !== 'off' ? <div className="cron-schedule-fields">
        <TimePicker label={`${label}时间`} value={time} onChange={updateTime} />
        {mode === 'monthly' && <div className="cron-select cron-select--day"><ElegantSelect id={daySelectId} value={day} options={Array.from({ length: 31 }, (_, index) => ({ value: String(index + 1), label: `${index + 1}日` }))} onChange={updateDay} searchable={false} searchPlaceholder="" /></div>}
        {mode === 'weekly' && <div className="cron-select cron-select--day"><ElegantSelect id={daySelectId} value={weekday} options={weekdayOptions} onChange={updateWeekday} searchable={false} searchPlaceholder="" /></div>}
      </div> : <span className="cron-disabled">未启用</span>}
    </div>
    {value && <code className="cron-expression-preview">{value}</code>}
    {previewError && <p className="cron-feedback cron-feedback--error"><AlertTriangle size={14} />{previewError}</p>}
    {preview.length > 0 && <div className="cron-preview"><span>未来 5 次 · {timezone}</span>{preview.map((item) => <time key={item}>{item}</time>)}</div>}
  </div>
}

function cronMode(value: string): 'off' | 'daily' | 'weekly' | 'monthly' | 'custom' {
  if (!value.trim()) return 'off'
  const fields = value.trim().split(/\s+/)
  if (fields.length !== 5 || !/^\d+$/.test(fields[0]) || !/^\d+$/.test(fields[1]) || fields[3] !== '*') return 'custom'
  if (/^\d+$/.test(fields[2]) && fields[4] === '*') return 'monthly'
  if (fields[2] === '*' && /^\d$/.test(fields[4])) return 'weekly'
  if (fields[2] === '*' && fields[4] === '*') return 'daily'
  return 'custom'
}

function buildCron(mode: 'daily' | 'weekly' | 'monthly' | 'custom' | 'off', time: string, day: string, weekday: string, minute?: number, hour?: number) {
  const [parsedHour = '00', parsedMinute = '00'] = time.split(':')
  const hourValue = String(hour ?? Number(parsedHour)).padStart(2, '0')
  const minuteValue = String(minute ?? Number(parsedMinute)).padStart(2, '0')
  if (mode === 'monthly') return `${Number(minuteValue)} ${Number(hourValue)} ${day} * *`
  if (mode === 'weekly') return `${Number(minuteValue)} ${Number(hourValue)} * * ${weekday}`
  return `${Number(minuteValue)} ${Number(hourValue)} * * *`
}

function formatScheduleTime(value: string, timezone: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('sv-SE', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(date)
}

function ScheduleReportNote({ stopTime }: { stopTime: string }) {
  return <p className="report-time-note"><Info size={15} aria-hidden="true" /><span>参与日报时，将在{stopTime ? <>每日 <strong>{stopTime}</strong> 关机时</> : '每日关机时'}单独推送本次运行的流量与账单。</span></p>
}

function DailyReportNote({ time }: { time?: string }) {
  return <p className="report-time-note"><Info size={15} aria-hidden="true" /><span>每日 <strong>{time || '00:00'}</strong> 推送前 24 小时的流量与账单；同一时间的普通日报合并发送。</span></p>
}

function NotificationSettings({ config, onChange, notify }: { config: Config; onChange: (config: Config) => void; notify: (message: string, tone?: Toast['tone']) => void }) {
  const [channel, setChannel] = useState<'email' | 'telegram' | 'webhook'>('email')
  const [testing, setTesting] = useState(false)
  const [testingDaily, setTestingDaily] = useState(false)
  const testDailyReport = async () => {
    setTestingDaily(true)
    try {
      const job = await api<Job>('/api/v1/notifications/daily-report', { method: 'POST', body: '{}' })
      await waitForJob(job.id)
      notify('日报测试已发送至已启用的通知渠道', 'success')
    } catch (error) {
      notify(error instanceof Error ? error.message : '发送日报测试失败', 'error')
    } finally {
      setTestingDaily(false)
    }
  }
  const [template, setTemplate] = useState('')
  const [modal, setModal] = useState<{ provider: string; name: string } | null>(null)
  const [form, setForm] = useState({ key: '', appToken: '', uid: '', token: '', secret: '' })
  const test = async () => {
    setTesting(true)
    try { const job = await api<Job>(`/api/v1/notifications/test/${channel}`, { method: 'POST', body: '{}' }); await waitForJob(job.id); notify('测试通知已送达', 'success') }
    catch (error) { notify(error instanceof Error ? error.message : '测试失败', 'error') }
    finally { setTesting(false) }
  }
  const n = config.notifications
  const updateWebhook = (patch: Partial<typeof n.webhook>) => onChange({ ...config, notifications: { ...n, webhook: { ...n.webhook, ...patch } } })
  const openTemplate = (provider: string) => {
    if (!provider) return
    const names: Record<string, string> = { bark: 'Bark', wxpusher: 'WxPusher', dingtalk: '钉钉群机器人', wecom: '微信群机器人' }
    setForm({ key: '', appToken: '', uid: '', token: '', secret: '' })
    setModal({ provider, name: names[provider] })
    setTemplate('')
  }
  const applyTemplate = () => {
    if (!modal) return
    const { provider } = modal
    if ((provider === 'bark' || provider === 'wecom') && !form.key) return notify('请填写 Key', 'error')
    if (provider === 'wxpusher' && (!form.appToken || !form.uid)) return notify('请填写 AppToken 和 UID', 'error')
    if (provider === 'dingtalk' && !form.token) return notify('请填写机器人 Access Token', 'error')
    const common = { enabled: true, provider, secret: form.secret || '' }
    if (provider === 'bark') updateWebhook({ ...common, method: 'GET', request_type: 'JSON', headers: '', url: `https://api.day.app/${encodeURIComponent(form.key)}/#TITLE#/#MSG#`, body: '' })
    if (provider === 'wxpusher') updateWebhook({ ...common, method: 'POST', request_type: 'JSON', headers: '', url: 'https://wxpusher.zjiecode.com/api/send/message', body: JSON.stringify({ appToken: form.appToken, content: '#MSG#', summary: '#TITLE#', contentType: 1, uids: [form.uid] }, null, 2) })
    if (provider === 'dingtalk') updateWebhook({ ...common, method: 'POST', request_type: 'JSON', headers: '', url: `https://oapi.dingtalk.com/robot/send?access_token=${encodeURIComponent(form.token)}`, body: JSON.stringify({ msgtype: 'text', text: { content: '#MSG#' } }, null, 2) })
    if (provider === 'wecom') updateWebhook({ ...common, method: 'POST', request_type: 'JSON', headers: '', url: `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=${encodeURIComponent(form.key)}`, body: JSON.stringify({ msgtype: 'text', text: { content: '#MSG#' } }, null, 2) })
    setChannel('webhook'); setModal(null); notify(`${modal.name} 模板已生成，请检查后保存`, 'success')
  }
  return <div className="settings-section"><SectionTitle icon={<Bell />} title="通知通道" subtitle="DELIVERY CHANNELS" />
    <div className="channel-tabs"><button className={channel === 'email' ? 'active' : ''} onClick={() => setChannel('email')}><Mail />Email</button><button className={channel === 'telegram' ? 'active' : ''} onClick={() => setChannel('telegram')}><Zap />Telegram</button><button className={channel === 'webhook' ? 'active' : ''} onClick={() => setChannel('webhook')}><Webhook />Webhook</button></div>
    {channel === 'webhook' && <><WebhookVariablePicker body={n.webhook.body} onBodyChange={(body) => updateWebhook({ body })} /><div className="webhook-template-bar"><div><b>快速生成 Webhook</b><small>选择渠道后填写关键配置</small></div><ElegantSelect id="webhook-template" value={template} options={[{ value: '', label: '手工配置' }, { value: 'bark', label: 'Bark' }, { value: 'wxpusher', label: 'WxPusher' }, { value: 'dingtalk', label: '钉钉群机器人' }, { value: 'wecom', label: '微信群机器人' }]} onChange={openTemplate} searchable={false} searchPlaceholder="" /></div></>}
    {channel === 'email' && <div className="form-grid settings-form"><ToggleRow title="启用 Email" icon={<Mail />} checked={n.email.enabled} onChange={(enabled) => onChange({ ...config, notifications: { ...n, email: { ...n.email, enabled } } })} /><Field label="接收邮箱"><input type="email" value={n.email.to} onChange={(event) => onChange({ ...config, notifications: { ...n, email: { ...n.email, to: event.target.value } } })} /></Field><Field label="SMTP Host"><input value={n.email.host} onChange={(event) => onChange({ ...config, notifications: { ...n, email: { ...n.email, host: event.target.value } } })} /></Field><Field label="端口"><input type="number" value={n.email.port} onChange={(event) => onChange({ ...config, notifications: { ...n, email: { ...n.email, port: Number(event.target.value) } } })} /></Field><SelectField label="安全模式" value={n.email.security} options={[{ value: 'ssl', label: 'SSL' }, { value: 'tls', label: 'STARTTLS' }, { value: 'none', label: '无' }]} onChange={(value) => onChange({ ...config, notifications: { ...n, email: { ...n.email, security: value } } })} /><Field label="用户名"><input value={n.email.username} onChange={(event) => onChange({ ...config, notifications: { ...n, email: { ...n.email, username: event.target.value } } })} /></Field><Field label={`密码${n.email.password_configured ? ' · 已配置' : ''}`}><input type="password" value={n.email.password || ''} placeholder={n.email.password_configured ? '留空保持不变' : ''} onChange={(event) => onChange({ ...config, notifications: { ...n, email: { ...n.email, password: event.target.value } } })} /></Field></div>}
    {channel === 'telegram' && <div className="form-grid settings-form"><ToggleRow title="启用 Telegram" icon={<Zap />} checked={n.telegram.enabled} onChange={(enabled) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, enabled } } })} /><Field label={`Bot Token${n.telegram.token_configured ? ' · 已配置' : ''}`}><input type="password" value={n.telegram.token || ''} placeholder={n.telegram.token_configured ? '留空保持不变' : ''} onChange={(event) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, token: event.target.value } } })} /></Field><Field label="Chat ID"><input value={n.telegram.chat_id} onChange={(event) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, chat_id: event.target.value } } })} /></Field><SelectField label="代理类型" value={n.telegram.proxy_type} options={[{ value: 'none', label: '直连' }, { value: 'custom', label: '自定义反代' }, { value: 'socks5', label: 'SOCKS5' }]} onChange={(value) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, proxy_type: value } } })} />{n.telegram.proxy_type === 'custom' && <Field label="反代 URL"><input value={n.telegram.proxy_url} onChange={(event) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, proxy_url: event.target.value } } })} /></Field>}{n.telegram.proxy_type === 'socks5' && <><Field label="代理 IP"><input value={n.telegram.proxy_ip} onChange={(event) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, proxy_ip: event.target.value } } })} /></Field><Field label="代理端口"><input value={n.telegram.proxy_port} onChange={(event) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, proxy_port: event.target.value } } })} /></Field><Field label="代理账号"><input value={n.telegram.proxy_user} onChange={(event) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, proxy_user: event.target.value } } })} /></Field><Field label={`代理密码${n.telegram.proxy_password_configured ? ' · 已配置' : ''}`}><input type="password" value={n.telegram.proxy_pass || ''} onChange={(event) => onChange({ ...config, notifications: { ...n, telegram: { ...n.telegram, proxy_pass: event.target.value } } })} /></Field></>}</div>}
    {channel === 'webhook' && <div className="form-grid settings-form"><ToggleRow title="启用 Webhook" icon={<Webhook />} checked={n.webhook.enabled} onChange={(enabled) => updateWebhook({ enabled })} /><Field label="Webhook URL"><input value={n.webhook.url} onChange={(event) => updateWebhook({ url: event.target.value })} /></Field>{n.webhook.provider === 'dingtalk' && <Field label={`钉钉加签密钥${n.webhook.secret_configured ? ' · 已配置' : ''}`}><input type="password" value={n.webhook.secret || ''} placeholder={n.webhook.secret_configured ? '留空保持不变' : '可选'} onChange={(event) => updateWebhook({ secret: event.target.value })} /></Field>}<SelectField label="请求方式" value={n.webhook.method} options={[{ value: 'GET', label: 'GET' }, { value: 'POST', label: 'POST' }]} onChange={(value) => updateWebhook({ method: value })} /><SelectField label="请求类型" value={n.webhook.request_type} options={[{ value: 'JSON', label: 'JSON' }, { value: 'FORM', label: 'FORM' }]} onChange={(value) => updateWebhook({ request_type: value })} /><Field label="自定义 Headers"><textarea value={n.webhook.headers || ''} onChange={(event) => updateWebhook({ headers: event.target.value })} placeholder='{"Authorization":"Bearer …"}' /></Field><Field label="Body 模板"><textarea value={n.webhook.body} onChange={(event) => updateWebhook({ body: event.target.value })} placeholder='{"title":"#TITLE#","message":"#MSG#"}' /></Field></div>}
    <div className="notification-action-row">
      <button className="button button--secondary" disabled={testing} onClick={() => void test()}>{testing ? <LoaderCircle className="spin" /> : <Bell />}发送测试</button>
      <button className="button button--secondary" disabled={testingDaily} onClick={() => void testDailyReport()}>{testingDaily ? <LoaderCircle className="spin" /> : <FileText />}发送日报测试</button>
    </div>
    {modal && <WebhookTemplateModal name={modal.name} provider={modal.provider} form={form} onChange={setForm} onClose={() => setModal(null)} onApply={applyTemplate} />}
  </div>
}

function WebhookVariablePicker({ body, onBodyChange }: { body: string; onBodyChange: (body: string) => void }) {
  const variables = [
    ['#TITLE#', '标题'], ['#MSG#', '消息'], ['#ACCOUNT#', '账号 ID'], ['#TRAFFIC#', '流量 GB'],
    ['#MAX_TRAFFIC#', '阈值 %'], ['#INSTANCE#', '实例 ID'], ['#STATUS#', '实例状态'], ['#TYPE#', '事件类型'],
    ['#CREATED_AT#', 'UTC 时间'],
  ]
  const insert = (value: string) => onBodyChange(body + (body && !body.endsWith('\n') ? '\n' : '') + value)
  return <div className="webhook-variables"><div className="webhook-variables__head"><span>可用变量</span><small>点击插入到 Body 末尾，也可直接写入 URL</small></div><div className="webhook-variables__list">{variables.map(([value, label]) => <button type="button" key={value} onClick={() => insert(value)} title={`插入 ${value}`}><code>{value}</code><span>{label}</span></button>)}</div></div>
}

function WebhookTemplateModal({ name, provider, form, onChange, onClose, onApply }: { name: string; provider: string; form: { key: string; appToken: string; uid: string; token: string; secret: string }; onChange: (value: typeof form) => void; onClose: () => void; onApply: () => void }) {
  const field = (key: keyof typeof form, label: string, type = 'text', placeholder = '') => <Field label={label}><input type={type} value={form[key]} placeholder={placeholder} onChange={(event) => onChange({ ...form, [key]: event.target.value })} /></Field>
  return <div className="modal-layer modal-layer--nested" role="dialog" aria-modal="true" aria-label={`${name} 模板配置`}><div className="modal-scrim" onClick={onClose} /><section className="glass-card webhook-template-modal"><header><div><p className="eyebrow">WEBHOOK TEMPLATE</p><h2>配置 {name}</h2></div><IconButton label="关闭" onClick={onClose}><X /></IconButton></header><p className="muted">填写渠道关键配置后，系统会自动生成 URL、Headers 和 Body。</p><div className="form-grid settings-form">{provider === 'bark' && field('key', 'Bark Key', 'text', '设备 Key')}{provider === 'wxpusher' && <>{field('appToken', 'AppToken', 'text', 'AT_…')}{field('uid', 'UID', 'text', 'UID_…')}</>}{provider === 'dingtalk' && <>{field('token', '机器人 Access Token', 'text', 'access_token')}{field('secret', '加签密钥（可选）', 'password', 'SEC…')}</>}{provider === 'wecom' && field('key', '微信群机器人 Key', 'text', 'xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx')}</div><footer className="settings-footer"><button className="button button--secondary" onClick={onClose}>取消</button><button className="button button--primary" onClick={onApply}><Check />生成 Webhook</button></footer></section></div>
}

function APIKeySettings({ notify }: { notify: (message: string, tone?: Toast['tone']) => void }) {
  const [keys, setKeys] = useState<APIKeyRecord[]>([])
  const [name, setName] = useState('桌面小组件')
  const [scopes, setScopes] = useState(['widget:read'])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const value = await api<{ keys?: APIKeyRecord[] }>('/api/v1/api-keys')
      setKeys(Array.isArray(value.keys) ? value.keys.filter((key) => !key.revoked_at) : [])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'API Key 列表加载失败')
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => { void load() }, [load])
  const create = async () => {
    try { const result = await api<{ token: string }>('/api/v1/api-keys', { method: 'POST', body: JSON.stringify({ name, scopes }) }); setToken(result.token); await load() }
    catch (error) { notify(error instanceof Error ? error.message : '创建失败', 'error') }
  }
  const toggle = (scope: string) => setScopes((current) => current.includes(scope) ? current.filter((value) => value !== scope) : [...current, scope])
  const revoke = async (id: number) => { try { await api(`/api/v1/api-keys/${id}`, { method: 'DELETE', body: '{}' }); await load(); notify('API Key 已撤销', 'success') } catch (cause) { notify(cause instanceof Error ? cause.message : '撤销失败', 'error') } }
  return <div className="settings-section"><SectionTitle icon={<KeyRound />} title="API Key" subtitle="MOBILE & WIDGET ACCESS" />
    <div className="key-create"><Field label="名称"><input value={name} onChange={(event) => setName(event.target.value)} /></Field><div className="scope-row">{[['widget:read', '读取状态'], ['instance:control', '控制实例'], ['cron:run', '触发任务']].map(([scope, label]) => <label key={scope} className={scopes.includes(scope) ? 'scope-chip active' : 'scope-chip'}><input type="checkbox" checked={scopes.includes(scope)} onChange={() => toggle(scope)} />{label}</label>)}</div><button className="button button--primary" disabled={!name || scopes.length === 0} onClick={() => void create()}><Plus />创建 Key</button></div>
    {token && <div className="token-reveal"><ShieldCheck /><div className="token-reveal__body"><b>仅显示一次</b><code>{token}</code></div><IconButton label="复制" onClick={() => { void navigator.clipboard.writeText(token); notify('已复制到剪贴板', 'success') }}><Copy /></IconButton></div>}
    {error && <div className="inline-error"><AlertTriangle size={16} />{error}<button className="text-button" onClick={() => void load()}>重试</button></div>}
    {loading ? <div className="subtle-empty"><LoaderCircle className="spin" />加载 API Key</div> : <div className="key-list">{keys.map((key) => <div className={`key-row ${key.revoked_at ? 'disabled' : ''}`} key={key.id}><div className="key-icon"><KeyRound /></div><div><b>{key.name}</b><span>{(Array.isArray(key.scopes) ? key.scopes : []).join(' · ') || '未配置权限'}</span></div><time>{key.last_used_at ? `最近使用 ${formatDate(key.last_used_at)}` : `创建于 ${formatDate(key.created_at)}`}</time>{!key.revoked_at && <IconButton label="撤销" tone="danger" onClick={() => void revoke(key.id)}><Trash2 /></IconButton>}</div>)}</div>}
  </div>
}

function LogSettings({ notify }: { notify: (message: string, tone?: Toast['tone']) => void }) {
  const [tab, setTab] = useState<'action' | 'heartbeat'>('action')
  const [logs, setLogs] = useState<LogEntry[]>([])
  const load = useCallback(async () => {
    try {
      const value = await api<{ logs?: LogEntry[] | null }>(`/api/v1/logs?tab=${tab}`)
      setLogs(Array.isArray(value.logs) ? value.logs : [])
    } catch (cause) {
      setLogs([])
      notify(cause instanceof Error ? cause.message : '日志加载失败', 'error')
    }
  }, [notify, tab])
  useEffect(() => { void load() }, [load])
  const clear = async () => { try { await api(`/api/v1/logs?tab=${tab}`, { method: 'DELETE', body: '{}' }); setLogs([]); await load(); notify('日志已清空', 'success') } catch (cause) { notify(cause instanceof Error ? cause.message : '日志清理失败', 'error') } }
  return <div className="settings-section"><div className="section-title-row"><SectionTitle icon={<FileClock />} title="运行日志" subtitle="EVENT STREAM" /><div className="log-actions"><Segmented value={tab} options={[['action', '动作'], ['heartbeat', '心跳']]} onChange={(value) => setTab(value as typeof tab)} /><IconButton label="清空" tone="danger" onClick={() => void clear()}><Trash2 /></IconButton></div></div><div className="log-list">{logs.length === 0 && <div className="subtle-empty"><FileClock />暂无日志</div>}{logs.map((log) => <div className="log-row" key={log.id}><i className={`log-dot log-dot--${log.type}`} /><div><p>{log.message}</p><span>{formatDate(log.created_at)} · {log.type.toUpperCase()}</span></div></div>)}</div></div>
}

function AdminSettingsPanel({ onClose, notify }: { onClose: () => void; notify: (message: string, tone?: Toast['tone']) => void }) {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordBusy, setPasswordBusy] = useState(false)
  const [passkeys, setPasskeys] = useState<PasskeyRecord[]>([])
  const [passkeyName, setPasskeyName] = useState('当前设备')
  const [passkeyBusy, setPasskeyBusy] = useState(false)
  const loadPasskeys = useCallback(async () => {
    try {
      const result = await api<{ passkeys?: PasskeyRecord[] }>('/api/v1/admin/passkeys')
      setPasskeys(Array.isArray(result.passkeys) ? result.passkeys : [])
    } catch (cause) { notify(cause instanceof Error ? cause.message : 'Passkey 列表加载失败', 'error') }
  }, [notify])
  useEffect(() => { void loadPasskeys() }, [loadPasskeys])
  const updatePassword = async () => {
    if (newPassword !== confirmPassword) { notify('两次新密码不一致', 'error'); return }
    setPasswordBusy(true)
    try {
      await api('/api/v1/admin/password', { method: 'PUT', body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }) })
      setCurrentPassword(''); setNewPassword(''); setConfirmPassword(''); notify('管理员密码已更新', 'success')
    } catch (cause) { notify(cause instanceof Error ? cause.message : '密码更新失败', 'error') }
    finally { setPasswordBusy(false) }
  }
  const createPasskey = async () => {
    if (!passkeyAvailable()) { notify('请使用 HTTPS 打开控制台后再创建 Passkey', 'error'); return }
    setPasskeyBusy(true)
    try {
      const begin = await api<PasskeyCeremony>('/api/v1/admin/passkeys/register/begin', { method: 'POST', body: JSON.stringify({ name: passkeyName }) })
      const credential = await navigator.credentials.create({ publicKey: decodeCreationOptions(begin.public_key.publicKey) })
      if (!(credential instanceof PublicKeyCredential)) throw new Error('未创建 Passkey')
      await api(`/api/v1/admin/passkeys/register/complete?session_id=${encodeURIComponent(begin.session_id)}`, { method: 'POST', body: JSON.stringify(serializeCredential(credential)) })
      await loadPasskeys(); notify('Passkey 创建成功', 'success')
    } catch (cause) { notify(cause instanceof Error ? cause.message : 'Passkey 创建失败', 'error') }
    finally { setPasskeyBusy(false) }
  }
  const removePasskey = async (id: number) => {
    try { await api(`/api/v1/admin/passkeys/${id}`, { method: 'DELETE', body: '{}' }); await loadPasskeys(); notify('Passkey 已删除', 'success') }
    catch (cause) { notify(cause instanceof Error ? cause.message : 'Passkey 删除失败', 'error') }
  }
  return <div className="modal-layer" role="dialog" aria-modal="true" aria-label="管理员设置"><div className="modal-scrim" onClick={onClose} /><section className="settings-panel admin-settings-panel">
    <header><div><p className="eyebrow">ADMINISTRATION</p><h2>管理员设置</h2></div><IconButton label="关闭" onClick={onClose}><X /></IconButton></header>
    <div className="settings-content"><div className="settings-section admin-settings-content">
      <section className="admin-block"><SectionTitle icon={<LockKeyhole />} title="修改管理员密码" subtitle="PASSWORD ROTATION" /><div className="form-grid settings-form"><Field label="当前密码"><input type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} /></Field><Field label="新密码"><input type="password" autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></Field><Field label="确认新密码"><input type="password" autoComplete="new-password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} /></Field></div><button className="button button--primary" disabled={passwordBusy || !currentPassword || !newPassword || !confirmPassword} onClick={() => void updatePassword()}>{passwordBusy ? <LoaderCircle className="spin" /> : <Save />}保存新密码</button></section>
      <section className="admin-block"><SectionTitle icon={<Fingerprint />} title="Passkey" subtitle="HTTPS DEVICE AUTHENTICATORS" /><p className="muted admin-note">Passkey 私钥只保存在设备或密码管理器中，服务端仅保存公钥。创建和登录必须使用 HTTPS。</p><div className="passkey-create"><Field label="设备名称"><input value={passkeyName} onChange={(event) => setPasskeyName(event.target.value)} placeholder="办公室电脑" /></Field><button className="button button--secondary" disabled={passkeyBusy || !passkeyAvailable()} onClick={() => void createPasskey()}>{passkeyBusy ? <LoaderCircle className="spin" /> : <Fingerprint />}创建 Passkey</button></div>{!passkeyAvailable() && <p className="inline-hint">当前连接不是 HTTPS，Passkey 创建按钮已禁用。</p>}<div className="passkey-list">{passkeys.length === 0 ? <div className="subtle-empty"><Fingerprint />尚未创建 Passkey</div> : passkeys.map((passkey) => <div className="passkey-row" key={passkey.id}><Fingerprint /><div><b>{passkey.name}</b><span>{passkey.last_used_at ? `最近使用 ${formatDate(passkey.last_used_at)}` : `创建于 ${formatDate(passkey.created_at)}`}</span></div><IconButton label="删除 Passkey" tone="danger" onClick={() => void removePasskey(passkey.id)}><Trash2 /></IconButton></div>)}</div></section>
    </div></div>
  </section></div>
}

function AboutSettings({ notify }: { notify: (message: string, tone?: Toast['tone']) => void }) {
  const [info, setInfo] = useState<SystemInfo | null>(null)
  const [checking, setChecking] = useState(false)
  useEffect(() => { void api<SystemInfo>('/api/v1/system/info').then(setInfo).catch((cause) => notify(cause instanceof Error ? cause.message : '版本信息加载失败', 'error')) }, [notify])
  const checkVersion = async () => {
    setChecking(true)
    try {
      let baseInfo: SystemInfo | null = null
      let failureMessage = '版本检查失败'
      try {
        const next = await api<SystemInfo>('/api/v1/system/info?check=1')
        baseInfo = next
        if (!next.check_error) {
          setInfo(next)
          notify('版本检查完成', 'success')
          return
        }
        failureMessage = next.check_error
      } catch (cause) {
        failureMessage = cause instanceof Error ? cause.message : '版本检查失败'
        baseInfo = info
      }
      if (!baseInfo) {
        notify(failureMessage, 'error')
        return
      }
      try {
        const latest = await fetchLatestReleaseFromGitHub()
        setInfo({ ...baseInfo, latest_version: latest, check_error: undefined })
        notify('已通过浏览器网络检查版本', 'success')
      } catch {
        setInfo(baseInfo)
        notify(failureMessage, 'error')
      }
    } finally {
      setChecking(false)
    }
  }
  return <div className="settings-section about-section"><SectionTitle icon={<Info />} title="关于 CDT Monitor" subtitle="PROJECT INFORMATION" /><div className="about-version"><div><span>当前版本</span><b>{info?.version || '加载中...'}</b><small>{info?.commit && info.commit !== 'unknown' ? `${info.commit} · ${info.built_at}` : '构建信息未知'}</small></div><button className="button button--secondary button--small" onClick={() => void checkVersion()} disabled={checking}>{checking ? <LoaderCircle className="spin" /> : <RefreshCw />}检查更新</button></div>{info?.latest_version && <p className="inline-hint">GitHub 最新版本：{info.latest_version}{info.latest_version === info.version ? '，当前已是最新版本' : '，请查看发布页获取更新'}</p>}<div className="about-links"><a href="https://github.com/wang4386/CDT-Monitor" target="_blank" rel="noreferrer"><SiteFavicon domain="github.com" label="GitHub" /><span><b>GitHub 仓库</b><small>源代码、Issue 与 Release</small></span><ExternalLink /></a><a href="https://qninq.cn" target="_blank" rel="noreferrer"><SiteFavicon domain="qninq.cn" label="qninq.cn" /><span><b>作者博客</b><small>qninq.cn</small></span><ExternalLink /></a><a href="https://www.nodeseek.com/" target="_blank" rel="noreferrer"><SiteFavicon domain="nodeseek.com" label="NodeSeek" /><span><b>NodeSeek</b><small>社区交流</small></span><ExternalLink /></a><a href="https://linux.do/" target="_blank" rel="noreferrer"><SiteFavicon domain="linux.do" label="linux.do" /><span><b>Linux.do</b><small>技术社区交流</small></span><ExternalLink /></a></div></div>
}

function SiteFavicon({ domain, label }: { domain: string; label: string }) {
  const [state, setState] = useState<'loading' | 'loaded' | 'failed'>('loading')
  return <span className="about-link__favicon" data-state={state}><img className={state === 'failed' ? 'is-hidden' : ''} src={`https://a.favicon.im/${domain}`} alt={`${label} favicon`} loading="lazy" decoding="async" referrerPolicy="no-referrer" onLoad={() => setState('loaded')} onError={() => setState('failed')} />{state === 'loading' && <LoaderCircle className="spin" aria-hidden="true" />}{state === 'failed' && <Globe2 aria-hidden="true" />}</span>
}

function HistoryModal({ account, onClose }: { account: AccountSummary; onClose: () => void }) {
  const [history, setHistory] = useState<History | null>(null)
  const [range, setRange] = useState<'hourly' | 'daily'>('hourly')
  useEffect(() => { void api<History>(`/api/v1/accounts/${account.id}/history`).then(setHistory) }, [account.id])
  const data = (history?.[range] || []).map((point) => ({ at: new Date(point.at).getTime(), traffic: Math.round(point.traffic * 1000) / 1000 }))
  return <div className="modal-layer" role="dialog" aria-modal="true"><div className="modal-scrim" onClick={onClose} /><section className="chart-modal glass-card"><header><div><p className="eyebrow">TRAFFIC HISTORY</p><h2>{account.remark || account.account}</h2></div><IconButton label="关闭" onClick={onClose}><X /></IconButton></header><Segmented value={range} options={[['hourly', '24 小时'], ['daily', '30 天']]} onChange={(value) => setRange(value as typeof range)} /><div className="chart-area" aria-label="流量历史图表">{!history ? <LoaderCircle className="spin chart-loader" /> : data.length === 0 ? <div className="subtle-empty"><HistoryIcon />等待采样数据</div> : <Suspense fallback={<LoaderCircle className="spin chart-loader" />}><HistoryChart data={data} range={range} /></Suspense>}</div></section></div>
}

function BrandMark() { return <span className="brand-mark"><Cloud size={21} /></span> }
function StepDots({ current }: { current: number }) { return <div className="step-dots">{[0, 1, 2].map((step) => <i className={step <= current ? 'active' : ''} key={step} />)}</div> }
function Metric({ icon, label, value, suffix, tone = 'neutral' }: { icon: React.ReactNode; label: string; value: string; suffix: string; tone?: string }) { return <article className={`metric metric--${tone}`}><span className="metric-icon">{icon}</span><div><label>{label}</label><strong>{value}<small>{suffix}</small></strong></div></article> }
function SectionTitle({ icon, title, subtitle }: { icon: React.ReactNode; title: string; subtitle: string }) { return <div className="section-title"><span>{icon}</span><div><p>{subtitle}</p><h3>{title}</h3></div></div> }

function SelectField({ label, value, options, onChange, searchable = false, searchPlaceholder = '搜索选项' }: { label: string; value: string; options: SelectOption[]; onChange: (value: string) => void; searchable?: boolean; searchPlaceholder?: string }) {
  const id = useId()
  return <div className="field"><label htmlFor={id}>{label}</label><ElegantSelect id={id} value={value} options={options} onChange={onChange} searchable={searchable} searchPlaceholder={searchPlaceholder} /></div>
}

function ElegantSelect({ id, value, options, onChange, searchable, searchPlaceholder }: { id: string; value: string; options: SelectOption[]; onChange: (value: string) => void; searchable: boolean; searchPlaceholder: string }) {
  const listId = `${id}-listbox`
  const rootRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const [position, setPosition] = useState<SelectPosition | null>(null)
  const selected = options.find((option) => option.value === value)
  const normalizedQuery = query.trim().toLocaleLowerCase('zh-CN')
  const filteredOptions = searchable && normalizedQuery
    ? options.filter((option) => `${option.label} ${option.meta || ''} ${option.value}`.toLocaleLowerCase('zh-CN').includes(normalizedQuery))
    : options
  const activeOptionId = open && filteredOptions[activeIndex] ? `${listId}-option-${activeIndex}` : undefined

  const updatePosition = useCallback(() => {
    const root = rootRef.current
    if (!root) return
    const rect = root.getBoundingClientRect()
    const isMobile = window.matchMedia('(max-width: 640px)').matches
    const viewport = window.visualViewport
    const visibleTop = viewport?.offsetTop ?? 0
    const visibleBottom = Math.min(window.innerHeight, visibleTop + (viewport?.height ?? window.innerHeight))
    const modal = root.closest('.modal-layer')
    const footer = modal?.querySelector<HTMLElement>('.settings-footer, .instance-settings-modal > footer')
    const footerTop = footer?.getBoundingClientRect().top ?? visibleBottom
    const menuBottom = Math.min(visibleBottom - 12, footerTop - (isMobile ? 8 : 0))
    const availableBelow = Math.max(0, menuBottom - rect.bottom - 8)
    const availableAbove = Math.max(0, rect.top - visibleTop - 12)
    const visibleLeft = viewport?.offsetLeft ?? 0
    const visibleRight = Math.min(window.innerWidth, visibleLeft + (viewport?.width ?? window.innerWidth))
    const menuWidth = isMobile ? Math.min(Math.max(rect.width, 220), visibleRight - visibleLeft - 24) : rect.width
    const maxMenuHeight = isMobile ? 420 : 320
    const estimatedHeight = Math.min(maxMenuHeight, filteredOptions.length * 52 + 16)
    const placement = availableBelow >= estimatedHeight || availableBelow >= availableAbove ? 'down' : 'up'
    const available = placement === 'down' ? availableBelow : availableAbove
    const left = Math.min(Math.max(visibleLeft + 12, rect.left), Math.max(visibleLeft + 12, visibleRight - menuWidth - 12))
    setPosition({
      left,
      top: placement === 'down' ? rect.bottom + 8 : rect.top - 8,
      width: menuWidth,
      maxHeight: Math.max(1, Math.min(maxMenuHeight, available - 8)),
      placement,
    })
  }, [filteredOptions.length])

  useLayoutEffect(() => {
    if (!open) return
    updatePosition()
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
    window.visualViewport?.addEventListener('resize', updatePosition)
    window.visualViewport?.addEventListener('scroll', updatePosition)
    return () => {
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
      window.visualViewport?.removeEventListener('resize', updatePosition)
      window.visualViewport?.removeEventListener('scroll', updatePosition)
    }
  }, [open, updatePosition])

  useEffect(() => {
    if (!open) return
    const handleOutside = (event: PointerEvent) => {
      const target = event.target as Node
      if (!rootRef.current?.contains(target) && !menuRef.current?.contains(target)) {
        setOpen(false)
        setQuery('')
      }
    }
    document.addEventListener('pointerdown', handleOutside, true)
    return () => document.removeEventListener('pointerdown', handleOutside, true)
  }, [open])

  useEffect(() => {
    if (open && searchable) window.requestAnimationFrame(() => searchRef.current?.focus())
  }, [open, searchable])

  const openSelect = () => {
    setQuery('')
    setActiveIndex(Math.max(0, options.findIndex((option) => option.value === value)))
    setOpen(true)
  }
  const closeSelect = (restoreFocus = false) => {
    setOpen(false)
    setQuery('')
    if (restoreFocus) window.requestAnimationFrame(() => (rootRef.current?.querySelector('[role="combobox"]') as HTMLElement | null)?.focus())
  }
  const choose = (option: SelectOption) => {
    onChange(option.value)
    closeSelect(true)
  }
  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (!open && ['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(event.key)) {
      event.preventDefault()
      openSelect()
      return
    }
    if (!open) return
    if (event.key === 'Escape') {
      event.preventDefault()
      closeSelect(true)
    } else if (event.key === 'Tab') {
      closeSelect()
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      if (filteredOptions.length) setActiveIndex((index) => (index + (event.key === 'ArrowDown' ? 1 : -1) + filteredOptions.length) % filteredOptions.length)
    } else if (event.key === 'Enter' && filteredOptions[activeIndex]) {
      event.preventDefault()
      choose(filteredOptions[activeIndex])
    }
  }

  const menu = position && (
    <div ref={menuRef} id={listId} role="listbox" className={`select-popover select-popover--${position.placement}`} style={{ left: position.left, top: position.top, width: position.width, maxHeight: position.maxHeight }}>
      {filteredOptions.length ? filteredOptions.map((option, index) => (
        <button type="button" role="option" id={`${listId}-option-${index}`} aria-selected={option.value === value} className={`select-option ${index === activeIndex ? 'active' : ''}`} key={option.value} onMouseMove={() => setActiveIndex(index)} onClick={() => choose(option)}>
          <span className="select-option__label">{option.label}</span>
          <span className="select-option__aside">{option.meta && <code>{option.meta}</code>}{option.value === value && <Check aria-hidden="true" />}</span>
        </button>
      )) : <div className="select-empty"><Search aria-hidden="true" /><span>未找到匹配选项</span></div>}
    </div>
  )

  return <div className={`select-control ${open ? 'open' : ''}`} ref={rootRef}>
    {searchable && open ? <div className="select-search"><Search aria-hidden="true" /><input ref={searchRef} id={id} role="combobox" aria-autocomplete="list" aria-expanded="true" aria-controls={listId} aria-activedescendant={activeOptionId} autoComplete="off" spellCheck={false} value={query} placeholder={searchPlaceholder} onChange={(event) => { setQuery(event.target.value); setActiveIndex(0) }} onKeyDown={handleKeyDown} /><ChevronDown className="select-chevron" aria-hidden="true" /></div> : <button id={id} type="button" role="combobox" aria-haspopup="listbox" aria-expanded={open} aria-controls={listId} aria-activedescendant={activeOptionId} className="select-trigger" onClick={() => open ? closeSelect() : openSelect()} onKeyDown={handleKeyDown}><span className="select-trigger__label">{selected?.label || value || '请选择'}</span>{selected?.meta && <code>{selected.meta}</code>}<ChevronDown className="select-chevron" aria-hidden="true" /></button>}
    {open && menu && createPortal(menu, document.body)}
  </div>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  const id = useId()
  const linked = Children.map(children, (child) => {
    if (!isValidElement(child) || !['input', 'textarea'].includes(String(child.type))) return child
    const element = child as ReactElement<{ id?: string }>
    return cloneElement(element, { id: element.props.id || id })
  })
  return <div className="field"><label htmlFor={id}>{label}</label><div className="input-shell">{linked}</div></div>
}
function PasswordField({ label, value, visible, onVisible, onChange, autoComplete }: { label: string; value: string; visible: boolean; onVisible: () => void; onChange: (value: string) => void; autoComplete: string }) { return <Field label={label}><LockKeyhole size={17} /><input type={visible ? 'text' : 'password'} value={value} onChange={(event) => onChange(event.target.value)} autoComplete={autoComplete} /><button type="button" className="input-action" onClick={onVisible} aria-label={visible ? '隐藏密码' : '显示密码'}>{visible ? <EyeOff /> : <Eye />}</button></Field> }
function Segmented({ value, options, onChange }: { value: string; options: readonly (readonly [string, string])[]; onChange: (value: string) => void }) { return <div className="segmented">{options.map(([key, label]) => <button type="button" className={value === key ? 'active' : ''} key={key} onClick={() => onChange(key)}>{label}</button>)}</div> }
function ToggleRow({ title, icon, checked, onChange }: { title: string; icon: React.ReactNode; checked: boolean; onChange: (checked: boolean) => void }) { return <label className="toggle-row"><span>{icon}</span><b>{title}</b><input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} /><i className="toggle"><em /></i></label> }
function IconButton({ label, title, children, onClick, disabled, tone = 'default', className = '', ariaExpanded, ariaControls }: { label: string; title?: string; children: React.ReactNode; onClick: () => void; disabled?: boolean; tone?: string; className?: string; ariaExpanded?: boolean; ariaControls?: string }) { return <button className={`icon-button icon-button--${tone} ${className}`} aria-label={label} aria-expanded={ariaExpanded} aria-controls={ariaControls} title={title || label} onClick={onClick} disabled={disabled}>{children}</button> }
function ToastStack({ items }: { items: Toast[] }) { return <div className="toast-stack" aria-live="polite">{items.map((toast) => <div className={`toast toast--${toast.tone}`} key={toast.id}>{toast.tone === 'success' ? <Check /> : toast.tone === 'error' ? <AlertTriangle /> : <Activity />}{toast.message}</div>)}</div> }

function statusClass(status: string) { if (status === 'Running') return 'positive'; if (status === 'Stopped') return 'negative'; if (status === 'Starting' || status === 'Stopping' || status === 'Pending') return 'warning'; return 'neutral' }
function statusLabel(status: string) { return ({ Running: '运行中', Stopped: '已停止', Starting: '启动中', Stopping: '停止中', Pending: '等待中', Unknown: '未知' } as Record<string, string>)[status] || status }
function formatTime(value: string) { return new Date(value).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) }
function formatDate(value: string) { return new Date(value).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }) }
function passkeyAvailable() { return location.protocol === 'https:' && window.isSecureContext && 'PublicKeyCredential' in window && 'credentials' in navigator }
function decodeBase64(value: unknown) { if (typeof value !== 'string') return value; const binary = atob(value.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - value.length % 4) % 4)); return Uint8Array.from(binary, (character) => character.charCodeAt(0)).buffer }
function decodeRequestOptions(options: Record<string, unknown>): PublicKeyCredentialRequestOptions {
  const result: Record<string, unknown> = { ...options, challenge: decodeBase64(options.challenge) }
  if (Array.isArray(options.allowCredentials)) result.allowCredentials = options.allowCredentials.map((item) => ({ ...(item as Record<string, unknown>), id: decodeBase64((item as Record<string, unknown>).id) }))
  return result as unknown as PublicKeyCredentialRequestOptions
}
function decodeCreationOptions(options: Record<string, unknown>): PublicKeyCredentialCreationOptions {
  const user = (options.user || {}) as Record<string, unknown>
  const result: Record<string, unknown> = { ...options, challenge: decodeBase64(options.challenge), user: { ...user, id: decodeBase64(user.id) } }
  if (Array.isArray(options.excludeCredentials)) result.excludeCredentials = options.excludeCredentials.map((item) => ({ ...(item as Record<string, unknown>), id: decodeBase64((item as Record<string, unknown>).id) }))
  return result as unknown as PublicKeyCredentialCreationOptions
}
function base64url(value: ArrayBuffer) { const bytes = new Uint8Array(value); let binary = ''; bytes.forEach((byte) => { binary += String.fromCharCode(byte) }); return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '') }
function serializeCredential(credential: PublicKeyCredential) {
  const response = credential.response
  const payload: Record<string, unknown> = { id: credential.id, rawId: base64url(credential.rawId), type: credential.type, clientExtensionResults: credential.getClientExtensionResults(), response: { clientDataJSON: base64url(response.clientDataJSON) } }
  if (response instanceof AuthenticatorAttestationResponse) {
    payload.response = { ...payload.response as Record<string, unknown>, attestationObject: base64url(response.attestationObject), transports: response.getTransports?.() }
  } else if (response instanceof AuthenticatorAssertionResponse) {
    payload.response = { ...payload.response as Record<string, unknown>, authenticatorData: base64url(response.authenticatorData), signature: base64url(response.signature), userHandle: response.userHandle ? base64url(response.userHandle) : null }
  }
  return payload
}
