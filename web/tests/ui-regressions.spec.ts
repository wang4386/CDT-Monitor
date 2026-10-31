import { expect, test } from '@playwright/test'

const config = {
  traffic_threshold: 95,
  enable_schedule_notification: false,
  shutdown_mode: 'KeepCharging',
  threshold_action: 'stop_and_notify',
  keep_alive: false,
  api_interval: 600,
  enable_billing: true,
  timezone: 'Asia/Shanghai',
  enable_daily_report: false,
  daily_report_time: '22:00',
  notifications: {
    email: { enabled: false, to: '', host: '', port: 465, username: '', password_configured: false, security: 'ssl' },
    telegram: { enabled: false, token_configured: false, chat_id: '', proxy_type: 'none', proxy_url: '', proxy_ip: '', proxy_port: '', proxy_user: '', proxy_password_configured: false },
    webhook: { enabled: false, url: '', method: 'GET', request_type: 'JSON', body: '' },
  },
  accounts: [{
    id: 1,
    access_key_id: 'LTAI5test',
    secret_configured: true,
    region_id: 'cn-hongkong',
    instance_id: 'i-test',
    max_traffic: 200,
    schedule_enabled: false,
    start_time: '08:00',
    stop_time: '23:30',
    remark: '香港测试节点',
    site_type: 'china',
  }],
}

test('login brand is positioned above the authentication card', async ({ page }, testInfo) => {
  await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
  await page.route('**/api/v1/status', (route) => route.fulfill({ status: 401, json: { error: { code: 'unauthorized', message: '需要登录' } } }))
  await page.route('**/api/v1/config', (route) => route.fulfill({ status: 401, json: { error: { code: 'unauthorized', message: '需要登录' } } }))

  await page.goto('/')
  const brand = page.locator('.login-brand')
  const card = page.locator('.auth-card')
  await expect(page.getByText('阿里云 CDT 流量与实例自动化控制台')).toBeVisible()
  await expect(brand.locator('.brand-mark')).toBeVisible()
  const brandBox = await brand.boundingBox()
  const cardBox = await card.boundingBox()
  expect(brandBox).not.toBeNull()
  expect(cardBox).not.toBeNull()
  expect(brandBox!.y + brandBox!.height).toBeLessThanOrEqual(cardBox!.y)
  await page.screenshot({ path: testInfo.outputPath('login-brand-desktop.png'), fullPage: true })
})

test('dashboard remains contained across supported viewport widths', async ({ page }, testInfo) => {
  await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
  await page.route('**/api/v1/status', (route) => route.fulfill({ json: {
    accounts: [{
      id: 1,
      account: 'LTAI5te***',
      remark: '香港测试节点',
      region: 'cn-hongkong',
      region_name: '中国香港',
      flow_total: 200,
      flow_used: 12.5,
      percentage: 6.25,
      threshold: 95,
      over_threshold: false,
      instance_status: 'Running',
      last_updated: new Date().toISOString(),
      stale: false,
      monthly_cost: 23.456,
      balance: 123.45,
      currency: 'CNY',
    }],
    system_last_run: new Date().toISOString(),
  } }))
  await page.route('**/api/v1/config', (route) => route.fulfill({ json: config }))

  for (const viewport of [
    { width: 320, height: 720 },
    { width: 390, height: 844 },
    { width: 768, height: 1024 },
    { width: 1440, height: 900 },
  ]) {
    await page.setViewportSize(viewport)
    await page.goto('/')
    const title = page.getByRole('heading', { name: '资源控制台' })
    await expect(title).toBeVisible()
    if (viewport.width <= 640) {
      await page.waitForTimeout(700)
      const closedTitleBox = await title.boundingBox()
      const closedOverviewBox = await page.locator('.overview-head').boundingBox()
      expect(closedTitleBox).not.toBeNull()
      expect(closedOverviewBox).not.toBeNull()
      await page.getByRole('button', { name: '菜单' }).click()
      const actions = page.locator('.topbar-actions.open')
      const overview = page.locator('.overview-head')
      await expect(actions).toBeVisible()
      await page.waitForTimeout(300)
      const actionsBox = await actions.boundingBox()
      const titleBox = await title.boundingBox()
      const overviewBox = await overview.boundingBox()
      expect(actionsBox).not.toBeNull()
      expect(titleBox).not.toBeNull()
      expect(overviewBox).not.toBeNull()
      expect(Math.abs(titleBox!.y - closedTitleBox!.y)).toBeLessThanOrEqual(1)
      expect(Math.abs(overviewBox!.y - closedOverviewBox!.y)).toBeLessThanOrEqual(1)
      expect(actionsBox!.width).toBeGreaterThan(actionsBox!.height * 2)
      const overlapsTitle = actionsBox!.x < titleBox!.x + titleBox!.width
        && actionsBox!.x + actionsBox!.width > titleBox!.x
        && actionsBox!.y < titleBox!.y + titleBox!.height
        && actionsBox!.y + actionsBox!.height > titleBox!.y
      const layerState = await page.evaluate(() => {
        const menu = document.querySelector<HTMLElement>('.topbar-actions.open')
        const topbar = document.querySelector<HTMLElement>('.topbar')
        const overview = document.querySelector<HTMLElement>('.overview-head')
        return {
          menuZ: Number.parseInt(getComputedStyle(menu!).zIndex, 10) || 0,
          topbarZ: Number.parseInt(getComputedStyle(topbar!).zIndex, 10) || 0,
          overviewZ: Number.parseInt(getComputedStyle(overview!).zIndex, 10) || 0,
        }
      })
      expect(layerState.menuZ).toBeGreaterThan(0)
      expect(layerState.topbarZ).toBeGreaterThan(layerState.overviewZ)
      if (overlapsTitle) {
        const overlapCenter = {
          x: (Math.max(actionsBox!.x, titleBox!.x) + Math.min(actionsBox!.x + actionsBox!.width, titleBox!.x + titleBox!.width)) / 2,
          y: (Math.max(actionsBox!.y, titleBox!.y) + Math.min(actionsBox!.y + actionsBox!.height, titleBox!.y + titleBox!.height)) / 2,
        }
        const menuOwnsOverlap = await page.evaluate(({ x, y }) => Boolean(document.elementFromPoint(x, y)?.closest('.topbar-actions')), overlapCenter)
        expect(menuOwnsOverlap, `${viewport.width}px title paints above the menu`).toBe(true)
      }
      await page.screenshot({ path: testInfo.outputPath(`dashboard-menu-${viewport.width}.png`), fullPage: true })
      await page.getByRole('button', { name: '菜单' }).click()
    }
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(overflow, `${viewport.width}px viewport overflow`).toBeLessThanOrEqual(1)
    await page.screenshot({ path: testInfo.outputPath(`dashboard-${viewport.width}.png`), fullPage: true })
  }
})

test('IP card centers by viewport and both schedule editors expose advanced Cron mode', async ({ page }, testInfo) => {
	const account = {
		id: 1,
		account: 'LTAI5te***',
		remark: '香港测试节点',
		region: 'cn-hongkong',
		region_name: '中国香港',
		flow_total: 200,
		flow_used: 12.5,
		percentage: 6.25,
		threshold: 95,
		over_threshold: false,
		instance_status: 'Running',
		public_ip: '203.0.113.10',
		public_ipv6: '2001:db8::10',
		ip_updated_at: new Date().toISOString(),
		last_updated: new Date().toISOString(),
		stale: false,
		schedule_enabled: true,
		schedule_mode: 'simple',
		start_time: '08:00',
		stop_time: '23:30',
	}
	const accountConfig = { ...config, accounts: [{ ...config.accounts[0], schedule_enabled: true, schedule_mode: 'simple' as const }] }
	await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
	await page.route('**/api/v1/status', (route) => route.fulfill({ json: { accounts: [account], system_last_run: new Date().toISOString() } }))
	await page.route('**/api/v1/config', (route) => route.fulfill({ json: accountConfig }))

	await page.setViewportSize({ width: 1440, height: 900 })
	await page.goto('/')
	await page.waitForTimeout(800)
	await page.getByRole('button', { name: '查看公网 IP' }).click()
	const ipCard = page.locator('.public-ip-modal')
	await expect(ipCard).toBeVisible()
  await expect(page.getByRole('button', { name: '查看公网 IP' })).toHaveCSS('box-shadow', 'none')
	await page.waitForTimeout(300)
	await expect(ipCard.getByText('203.0.113.10')).toBeVisible()
	await expect(ipCard.getByText('2001:db8::10')).toBeVisible()
  const copyButton = ipCard.getByRole('button', { name: '复制 IPv4 地址' })
  await expect(copyButton).toHaveCSS('position', 'static')
  await expect(copyButton).toHaveCSS('transform', 'none')
	const desktopCenters = await page.evaluate(() => {
		const card = document.querySelector<HTMLElement>('.public-ip-modal')!.getBoundingClientRect()
		const instance = document.querySelector<HTMLElement>('.account-card')!.getBoundingClientRect()
		return { cardX: card.x + card.width / 2, cardY: card.y + card.height / 2, instanceX: instance.x + instance.width / 2, instanceY: instance.y + instance.height / 2 }
	})
	expect(Math.abs(desktopCenters.cardX - desktopCenters.instanceX)).toBeLessThanOrEqual(1)
	expect(Math.abs(desktopCenters.cardY - desktopCenters.instanceY)).toBeLessThanOrEqual(4)
	await page.screenshot({ path: testInfo.outputPath('ip-card-desktop.png') })
	await page.getByRole('button', { name: '关闭', exact: true }).click()

	await page.setViewportSize({ width: 390, height: 844 })
	await page.getByRole('button', { name: '查看公网 IP' }).click()
	await expect(ipCard).toBeVisible()
	await page.waitForTimeout(300)
	const mobileCenters = await page.evaluate(() => {
		const card = document.querySelector<HTMLElement>('.public-ip-modal')!.getBoundingClientRect()
		return { x: card.x + card.width / 2, y: card.y + card.height / 2, width: window.innerWidth, height: window.innerHeight }
	})
	expect(Math.abs(mobileCenters.x - mobileCenters.width / 2)).toBeLessThanOrEqual(1)
	expect(Math.abs(mobileCenters.y - mobileCenters.height / 2)).toBeLessThanOrEqual(1)
	await page.screenshot({ path: testInfo.outputPath('ip-card-mobile.png') })
	await page.getByRole('button', { name: '关闭', exact: true }).click()

	await page.getByRole('button', { name: '菜单' }).click()
	await page.getByRole('button', { name: '设置', exact: true }).click()
	await page.getByRole('button', { name: '实例', exact: true }).click()
		const scheduleMode = page.getByRole('group', { name: '定时任务模式' })
		await scheduleMode.getByRole('button', { name: '进阶' }).click()
		await expect(scheduleMode).toHaveAttribute('data-mode', 'advanced')
	await expect(page.locator('.cron-editor')).toHaveCount(2)
	await expect.poll(async () => page.locator('.cron-editor').evaluateAll((editors) => editors.every((editor) => editor.scrollWidth <= editor.clientWidth + 1))).toBe(true)
		await expect(page.locator('.cron-editor').first().locator('.cron-editor__head .cron-preview-button')).toBeVisible()
		await expect(page.locator('.cron-editor').first().locator('.cron-editor__head > svg')).toHaveCount(0)
		await page.screenshot({ path: testInfo.outputPath('cron-advanced-settings-desktop.png') })
		await page.locator('.cron-editor').first().locator('.cron-select:not(.cron-select--day) [role="combobox"]').click()
		await page.getByRole('option', { name: '每周', exact: true }).click()
		await page.locator('.cron-editor').first().locator('.cron-select--day [role="combobox"]').click()
		await expect(page.getByRole('option', { name: '周一', exact: true })).toBeVisible()
		await page.keyboard.press('Escape')
		await page.locator('.cron-editor').first().locator('.cron-select:not(.cron-select--day) [role="combobox"]').click()
		await page.getByRole('option', { name: '每月', exact: true }).click()
		await page.locator('.cron-editor').first().locator('.cron-select--day [role="combobox"]').click()
		await expect(page.getByRole('option', { name: '1日', exact: true })).toBeVisible()
		await expect(page.getByRole('option', { name: '每月 1 日', exact: true })).toHaveCount(0)
		await page.keyboard.press('Escape')
		const scheduleWidths = await page.locator('.cron-editor').first().evaluate((editor) => {
			const time = editor.querySelector<HTMLElement>('.time-picker')?.getBoundingClientRect()
			const day = editor.querySelector<HTMLElement>('.cron-select--day')?.getBoundingClientRect()
			return { time: time?.width ?? 0, day: day?.width ?? 0 }
		})
		expect(Math.abs(scheduleWidths.time - scheduleWidths.day)).toBeLessThanOrEqual(1)
	await page.getByRole('button', { name: '关闭', exact: true }).click()

	await page.getByRole('button', { name: '实例设置' }).click()
	await page.getByRole('group', { name: '定时任务模式' }).getByRole('button', { name: '进阶' }).click()
	await expect(page.locator('.cron-editor')).toHaveCount(2)
	const mobileCron = page.locator('.cron-editor').first()
	const modeSelect = mobileCron.locator('.cron-select:not(.cron-select--day) [role="combobox"]')
	const assertMobileSelectFits = async () => {
		const popover = page.locator('.select-popover')
		await expect(popover).toBeVisible()
		const bounds = await popover.evaluate((menu) => {
			const rect = menu.getBoundingClientRect()
			const footer = document.querySelector<HTMLElement>('.instance-settings-modal > footer')
			const footerTop = footer?.getBoundingClientRect().top ?? window.innerHeight
			const labelsFit = Array.from(menu.querySelectorAll<HTMLElement>('.select-option__label')).every((label) => label.scrollWidth <= label.clientWidth + 1)
			const optionsFit = Array.from(menu.querySelectorAll<HTMLElement>('[role="option"]')).every((option) => {
				const optionRect = option.getBoundingClientRect()
				return optionRect.top >= rect.top - 1 && optionRect.bottom <= rect.bottom + 1
			})
			return { width: rect.width, top: rect.top, bottom: rect.bottom, footerTop, labelsFit, optionsFit, scrollable: menu.scrollHeight > menu.clientHeight + 1 }
		})
		expect(bounds.width).toBeGreaterThanOrEqual(160)
		expect(bounds.width).toBeLessThanOrEqual(180)
		expect(bounds.top).toBeGreaterThanOrEqual(0)
		expect(bounds.bottom).toBeLessThanOrEqual(bounds.footerTop)
		expect(bounds.labelsFit).toBe(true)
		expect(bounds.optionsFit).toBe(true)
		expect(bounds.scrollable).toBe(false)
	}
	await modeSelect.click()
	await assertMobileSelectFits()
	await page.getByRole('option', { name: '每周', exact: true }).click()
	await mobileCron.locator('.cron-select--day [role="combobox"]').click()
	await assertMobileSelectFits()
	await expect(page.getByRole('option', { name: '周日', exact: true })).toBeVisible()
		await page.screenshot({ path: testInfo.outputPath('cron-advanced-instance-mobile-menu.png') })
	await page.keyboard.press('Escape')
		await page.screenshot({ path: testInfo.outputPath('cron-advanced-instance-mobile.png') })
})

test('top refresh forces every configured instance and reports completion', async ({ page }, testInfo) => {
  const accounts = [
    {
      id: 1, account: 'LTAI5on***', remark: '香港节点', region: 'cn-hongkong', region_name: '中国香港', flow_total: 200, flow_used: 12.5,
      percentage: 6.25, threshold: 95, over_threshold: false, instance_status: 'Running', last_updated: new Date().toISOString(), stale: false,
    },
    {
      id: 2, account: 'LTAI5tw***', remark: '新加坡节点', region: 'ap-southeast-1', region_name: '新加坡', flow_total: 300, flow_used: 8.25,
      percentage: 2.75, threshold: 95, over_threshold: false, instance_status: 'Stopped', last_updated: new Date().toISOString(), stale: false,
    },
  ]
  let statusRequests = 0
  let refreshAllCalls = 0
  const jobPolls = new Map<string, number>()
  await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
  await page.route('**/api/v1/status', (route) => { statusRequests += 1; return route.fulfill({ json: { accounts, system_last_run: new Date().toISOString() } }) })
  await page.route('**/api/v1/config', (route) => route.fulfill({ json: { ...config, accounts: [config.accounts[0], { ...config.accounts[0], id: 2, access_key_id: 'LTAI5test2', instance_id: 'i-test-2', remark: '新加坡节点', region_id: 'ap-southeast-1', site_type: 'international' }] } }))
  await page.route('**/api/v1/accounts/refresh', (route) => {
    refreshAllCalls += 1
    expect(route.request().method()).toBe('POST')
    return route.fulfill({ status: 202, json: { jobs: [{ id: 'refresh-1', status: 'pending' }, { id: 'refresh-2', status: 'pending' }] } })
  })
  await page.route('**/api/v1/jobs/**', (route) => {
    const id = route.request().url().split('/').pop() || ''
    const count = (jobPolls.get(id) || 0) + 1
    jobPolls.set(id, count)
    return route.fulfill({ json: { id, status: count > 1 ? 'completed' : 'running' } })
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await page.getByRole('button', { name: '菜单' }).click()
  await page.getByRole('button', { name: '强制刷新全部实例' }).click()
  await expect(page.getByRole('button', { name: '正在强制刷新全部实例' })).toBeDisabled()
  await expect(page.getByText('已强制刷新 2 个实例')).toBeVisible({ timeout: 10_000 })
  expect(refreshAllCalls).toBe(1)
  expect(statusRequests).toBeGreaterThanOrEqual(2)
  expect(jobPolls.get('refresh-1')).toBeGreaterThanOrEqual(2)
  expect(jobPolls.get('refresh-2')).toBeGreaterThanOrEqual(2)
  await page.screenshot({ path: testInfo.outputPath('dashboard-refresh-all-mobile.png'), fullPage: true })
})

test('about update check falls back to the browser GitHub request', async ({ page }) => {
  await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
  await page.route('**/api/v1/status', (route) => route.fulfill({ json: { accounts: [], system_last_run: new Date().toISOString() } }))
  await page.route('**/api/v1/config', (route) => route.fulfill({ json: config }))
  await page.route('**/api/v1/system/info**', (route) => {
    const check = new URL(route.request().url()).searchParams.get('check') === '1'
    return route.fulfill({ json: check
      ? { version: 'v2.0.2', commit: 'test', built_at: new Date().toISOString(), repository: 'https://github.com/wang4386/CDT-Monitor', release_url: 'https://github.com/wang4386/CDT-Monitor/releases', check_error: '暂时无法检查 GitHub Release' }
      : { version: 'v2.0.2', commit: 'test', built_at: new Date().toISOString(), repository: 'https://github.com/wang4386/CDT-Monitor', release_url: 'https://github.com/wang4386/CDT-Monitor/releases' } })
  })
  await page.route('https://api.github.com/repos/wang4386/CDT-Monitor/releases/latest', (route) => route.fulfill({ headers: { 'access-control-allow-origin': '*' }, json: { tag_name: 'v2.0.3' } }))

  await page.goto('/')
  await page.getByRole('button', { name: '设置', exact: true }).click()
  await page.getByRole('button', { name: '关于' }).click()
  await page.getByRole('button', { name: '检查更新' }).click()
  await expect(page.getByText('GitHub 最新版本：v2.0.3')).toBeVisible()
  await expect(page.getByText('已通过浏览器网络检查版本')).toBeVisible()
})

test('dashboard billing, history precision and settings remain usable', async ({ page }, testInfo) => {
  await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
  await page.route('**/api/v1/status', (route) => route.fulfill({ json: {
    accounts: [{
      id: 1,
      account: 'LTAI5te***',
      remark: '香港测试节点',
      region: 'cn-hongkong',
      region_name: '中国香港',
      flow_total: 200,
      flow_used: 12.5,
      percentage: 6.25,
      threshold: 95,
      over_threshold: false,
      instance_status: 'Running',
      last_updated: new Date().toISOString(),
      stale: false,
      monthly_cost: 23.456,
      balance: 123.45,
      currency: 'CNY',
    }],
    system_last_run: new Date().toISOString(),
  } }))
  await page.route('**/api/v1/config', (route) => route.fulfill({ json: config }))
  await page.route('**/api/v1/accounts/1/history', (route) => route.fulfill({ json: {
    hourly: [{ at: new Date().toISOString(), traffic: 1.23456 }],
    daily: [{ at: new Date().toISOString(), traffic: 9.87654 }],
  } }))
  const longToken = 'cdt_' + 'A1b2C3d4'.repeat(12)
  await page.route('**/api/v1/api-keys', (route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 201, json: { token: longToken } })
    }
    return route.fulfill({ json: {
      keys: [
        { id: 1, name: '旧版 Key', scopes: null, created_at: new Date().toISOString() },
        { id: 2, name: '已撤销 Key', scopes: ['widget:read'], created_at: new Date().toISOString(), revoked_at: new Date().toISOString() },
      ],
    } })
  })
  await page.route('**/api/v1/system/info**', (route) => route.fulfill({ json: { version: 'v2.0.1', commit: 'test', built_at: new Date().toISOString(), repository: 'https://github.com/wang4386/CDT-Monitor', release_url: 'https://github.com/wang4386/CDT-Monitor/releases', latest_version: 'v2.0.1' } }))
  await page.route('**/api/v1/admin/passkeys', (route) => route.fulfill({ json: { passkeys: [] } }))
  let logsCleared = false
  await page.route('**/api/v1/logs**', (route) => {
    if (route.request().method() === 'DELETE') {
      logsCleared = true
      return route.fulfill({ json: { success: true } })
    }
    return route.fulfill({ json: { logs: logsCleared ? null : [{ id: 1, type: 'audit', message: '这是一条用于验证移动端日志布局不会向右溢出的较长运行日志内容', created_at: new Date().toISOString() }] } })
  })

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')
  const billing = page.locator('.account-billing')
  await expect(billing.getByText('本月费用')).toBeVisible()
  await expect(billing.getByText('¥23.46')).toBeVisible()
  await expect(billing.getByText('¥123.45')).toBeVisible()
  await expect(page.locator('.billing-row')).toHaveCount(0)
  await expect(page.locator('.metric--blue .metric-icon')).toBeVisible()
  await expect(page.locator('.metric--green .metric-icon')).toBeVisible()
  await expect(page.locator('.metric--cyan .metric-icon')).toBeVisible()
  await expect(page.locator('.metric--amber .metric-icon')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('dashboard-billing-desktop.png'), fullPage: true })

  await page.getByRole('button', { name: '实例设置' }).click()
  const modal = page.locator('.instance-settings-modal')
  await expect(modal).toBeVisible()
  await expect(modal.getByText('抢占式保活')).toBeVisible()
  await expect(modal.getByText('默认停机方式')).toBeVisible()
	await expect(modal.getByText('定时任务编排')).toBeVisible()
  await expect(modal.getByText('参与日报推送')).toBeVisible()
  // Non-scheduled: daily report time input is visible
  await expect(modal.getByLabel('日报推送时间')).toBeVisible()
  // Enable schedule: daily report time picker is hidden, shutdown timing is explained
	await modal.getByRole('checkbox').check()
  await expect(modal.getByLabel('日报推送时间')).not.toBeVisible()
  await expect(modal.locator('.report-time-note')).toContainText('每日 23:30 关机时')
  await page.screenshot({ path: testInfo.outputPath('scheduled-report-note-desktop.png') })
  await modal.locator('header').getByRole('button', { name: '关闭' }).click()

  await page.getByRole('button', { name: '查看历史流量' }).click()
  await expect(page.locator('.chart-modal')).toBeVisible()
  const latestSample = page.locator('.chart-area .recharts-line-dot').last()
  await expect(latestSample).toBeVisible()
  await latestSample.hover()
  await expect(page.getByText('1.235')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('history-chart-desktop.png'), fullPage: true })
  await page.getByRole('button', { name: '30 天' }).click()
  const sparseBar = page.locator('.recharts-bar-rectangle .recharts-rectangle').first()
  await expect(sparseBar).toBeVisible()
  const sparseBarWidth = await sparseBar.evaluate((element) => (element as unknown as SVGGraphicsElement).getBBox().width)
  expect(sparseBarWidth).toBeLessThanOrEqual(26)
  await page.screenshot({ path: testInfo.outputPath('history-chart-daily-sparse.png'), fullPage: true })
  await page.getByRole('button', { name: '24 小时' }).click()
  await page.locator('.chart-modal').getByRole('button', { name: '关闭' }).click()

  await page.getByRole('button', { name: '设置', exact: true }).click()
  // Verify General Settings has daily report toggle but NO time setting option
  await expect(page.getByText('每日消费与流量日报')).toBeVisible()
  await expect(page.locator('.settings-panel').getByLabel('日报推送时间')).toHaveCount(0)
  const settingsSection = page.locator('.settings-section')
  const generalSectionWidth = await settingsSection.evaluate((element) => element.clientWidth)
  const refreshSelectDesktop = page.getByRole('combobox', { name: 'API 刷新间隔' })
  await expect(refreshSelectDesktop).toBeVisible()
  await refreshSelectDesktop.click()
  await expect(page.getByRole('listbox')).toBeVisible()
  await expect(page.getByRole('option', { name: '1 小时' })).toBeVisible()
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: '实例', exact: true }).click()
  const accountSectionWidth = await settingsSection.evaluate((element) => element.clientWidth)
  expect(Math.abs(accountSectionWidth - generalSectionWidth)).toBeLessThanOrEqual(1)
  const regionSelect = page.getByRole('combobox', { name: '地域' })
  await regionSelect.click()
  await page.getByRole('combobox', { name: '地域' }).fill('zhangjiakou')
  const zhangjiakou = page.getByRole('option', { name: /华北 3（张家口）.*cn-zhangjiakou/ })
  await expect(zhangjiakou).toBeVisible()
  await page.getByRole('combobox', { name: '地域' }).fill('首尔')
  await expect(page.getByRole('option', { name: /韩国（首尔）.*ap-northeast-2/ })).toBeVisible()
  await page.getByRole('combobox', { name: '地域' }).fill('zhangjiakou')
  await page.screenshot({ path: testInfo.outputPath('settings-region-select-desktop.png'), fullPage: true })
  await zhangjiakou.click()
  await expect(page.getByRole('combobox', { name: '地域' })).toContainText('cn-zhangjiakou')
  await expect(page.getByRole('button', { name: '复制' })).toBeVisible()
  await page.getByRole('button', { name: '复制' }).click()
  await expect(page.locator('.account-editor')).toHaveCount(2)

  await page.getByRole('button', { name: '通知', exact: true }).click()
  const notificationSectionWidth = await settingsSection.evaluate((element) => element.clientWidth)
  expect(Math.abs(notificationSectionWidth - generalSectionWidth)).toBeLessThanOrEqual(1)
  await page.getByRole('button', { name: 'Webhook' }).click()
  await page.getByTitle('插入 #TITLE#').click()
  await expect(page.getByLabel('Body 模板')).toHaveValue('#TITLE#')
  await expect(page.getByRole('button', { name: '发送日报测试' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('settings-select-desktop.png'), fullPage: true })

  await page.getByRole('button', { name: '关于' }).click()
  const aboutSectionWidth = await settingsSection.evaluate((element) => element.clientWidth)
  expect(Math.abs(aboutSectionWidth - generalSectionWidth)).toBeLessThanOrEqual(1)
  await expect(page.locator('.about-links a')).toHaveCount(4)
  await page.screenshot({ path: testInfo.outputPath('settings-about-desktop.png'), fullPage: true })
  await page.locator('.settings-panel').getByRole('button', { name: '关闭' }).click()

  for (const viewport of [{ width: 320, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport)
    await page.getByRole('button', { name: '查看历史流量' }).click()
    const mobileChart = page.locator('.chart-area')
    await expect(mobileChart).toBeVisible()
    const mobileTicks = page.locator('.chart-area .recharts-xAxis .recharts-cartesian-axis-tick text')
    await expect(mobileTicks).toHaveCount(5)
    const chartBox = await mobileChart.boundingBox()
    const tickBoxes = await mobileTicks.evaluateAll((ticks) => ticks.map((tick) => tick.getBoundingClientRect().toJSON()))
    expect(chartBox).not.toBeNull()
    expect(tickBoxes[0].x).toBeGreaterThanOrEqual(chartBox!.x)
    expect(tickBoxes[tickBoxes.length - 1].x + tickBoxes[tickBoxes.length - 1].width).toBeLessThanOrEqual(chartBox!.x + chartBox!.width)
    await page.screenshot({ path: testInfo.outputPath(`history-chart-${viewport.width}.png`), fullPage: true })
    await page.locator('.chart-modal').getByRole('button', { name: '关闭' }).click()
  }

  await page.setViewportSize({ width: 320, height: 720 })
  await page.getByRole('button', { name: '菜单' }).click()
  await page.getByRole('button', { name: '设置', exact: true }).click()
  const panel = page.locator('.settings-panel')
  await expect(panel).toBeVisible()
  const panelBox = await panel.boundingBox()
  expect(panelBox).not.toBeNull()
  expect(panelBox!.x).toBeGreaterThan(0)
  expect(panelBox!.y).toBeGreaterThan(0)
  expect(panelBox!.x + panelBox!.width).toBeLessThan(390)
  expect(panelBox!.y + panelBox!.height).toBeLessThan(844)
  expect(await panel.evaluate((element) => getComputedStyle(element).borderRadius)).not.toBe('0px')

  await page.getByRole('button', { name: '实例', exact: true }).click()
  await page.getByRole('combobox', { name: '地域' }).click()
  await page.getByRole('combobox', { name: '地域' }).fill('cn-')
  const mobileRegionMenu = page.getByRole('listbox')
  await expect(mobileRegionMenu).toBeVisible()
  const mobileRegionMenuBox = await mobileRegionMenu.boundingBox()
  expect(mobileRegionMenuBox).not.toBeNull()
  expect(mobileRegionMenuBox!.x).toBeGreaterThanOrEqual(0)
  expect(mobileRegionMenuBox!.x + mobileRegionMenuBox!.width).toBeLessThanOrEqual(390)
  await page.screenshot({ path: testInfo.outputPath('settings-region-select-mobile.png'), fullPage: true })
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: '日志' }).click()
  await expect(page.getByRole('heading', { name: '运行日志' })).toBeVisible()
  const contentOverflow = await page.locator('.settings-content').evaluate((element) => element.scrollWidth - element.clientWidth)
  expect(contentOverflow).toBeLessThanOrEqual(1)
  await page.getByRole('button', { name: '清空' }).click()
  await expect(page.getByText('暂无日志')).toBeVisible()
  await expect(page.getByRole('heading', { name: '运行日志' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('settings-logs-mobile.png'), fullPage: true })

  await page.getByRole('button', { name: 'API Key' }).click()
  await expect(page.getByRole('heading', { name: 'API Key' })).toBeVisible()
  await expect(page.getByText('旧版 Key')).toBeVisible()
  await expect(page.getByText('已撤销 Key')).toHaveCount(0)
  await expect(page.getByText('未配置权限')).toBeVisible()
  await page.getByRole('button', { name: '创建 Key' }).click()
  await expect(page.getByText('仅显示一次')).toBeVisible()
  await expect(page.locator('.token-reveal code')).toHaveText(longToken)
  const tokenOverflow = await page.locator('.token-reveal').evaluate((element) => element.scrollWidth - element.clientWidth)
  expect(tokenOverflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: testInfo.outputPath('settings-api-key-mobile.png'), fullPage: true })
  await page.getByRole('button', { name: '关于' }).click()
  await expect(page.getByRole('heading', { name: '关于 CDT Monitor' })).toBeVisible()
  await expect(page.locator('.about-version b')).toHaveText('v2.0.1')
  const updateButton = page.getByRole('button', { name: '检查更新' })
  await expect(updateButton).toBeVisible()
  const updateButtonStyle = await updateButton.evaluate((element) => ({
    whiteSpace: getComputedStyle(element).whiteSpace,
    width: element.getBoundingClientRect().width,
    height: element.getBoundingClientRect().height,
  }))
  expect(updateButtonStyle.whiteSpace).toBe('nowrap')
  expect(updateButtonStyle.width).toBeGreaterThan(updateButtonStyle.height * 2)
  await expect(page.getByRole('link', { name: /NodeSeek/ })).toBeVisible()
  const faviconSources = await page.locator('.about-link__favicon img').evaluateAll((images) => images.map((image) => image.getAttribute('src')))
  expect(faviconSources).toEqual([
    'https://a.favicon.im/github.com',
    'https://a.favicon.im/qninq.cn',
    'https://a.favicon.im/nodeseek.com',
    'https://a.favicon.im/linux.do',
  ])
  await expect.poll(() => page.locator('.about-link__favicon').evaluateAll((items) => items.every((item) => item.getAttribute('data-state') !== 'loading'))).toBe(true)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: testInfo.outputPath('settings-about-mobile.png'), fullPage: true })
  await panel.getByRole('button', { name: '关闭' }).click()
  await page.getByRole('button', { name: '菜单' }).click()
  await page.getByRole('button', { name: '管理员' }).click()
  await expect(page.getByRole('heading', { name: '管理员设置' })).toBeVisible()
  await expect(page.getByRole('button', { name: '创建 Passkey' })).toBeDisabled()
  const adminOverflow = await page.locator('.admin-settings-panel').evaluate((element) => element.scrollWidth - element.clientWidth)
  expect(adminOverflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: testInfo.outputPath('admin-settings-mobile.png'), fullPage: true })
})

test('time picker works in desktop settings and mobile schedule', async ({ page }, testInfo) => {
  await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
  await page.route('**/api/v1/status', (route) => route.fulfill({ json: { accounts: [], system_last_run: new Date().toISOString() } }))
  let savedConfig: typeof config | undefined
  await page.route('**/api/v1/config', (route) => {
    if (route.request().method() === 'PUT') {
      savedConfig = route.request().postDataJSON()
      return route.fulfill({ json: { success: true } })
    }
    return route.fulfill({ json: config })
  })

  await page.goto('/')
  await page.getByRole('button', { name: '设置', exact: true }).click()
  await page.getByRole('button', { name: '实例', exact: true }).click()
  const editor = page.locator('.account-editor')
  const daily = editor.getByRole('button', { name: '日报推送时间' })
  await daily.click()
  let picker = page.getByRole('dialog', { name: '日报推送时间，选择时间' })
  await expect(picker).toBeVisible()
  await picker.getByRole('button', { name: '03 时' }).click()
  await picker.getByRole('button', { name: '45 分' }).click()
  await page.screenshot({ path: testInfo.outputPath('time-picker-desktop.png') })
  await picker.getByRole('button', { name: '确定' }).click()
  await expect(daily).toContainText('03:45')
  await expect(editor.locator('.report-time-note')).toContainText('每日 03:45 推送')

  await page.setViewportSize({ width: 390, height: 844 })
  await editor.locator('.report-time-note').scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('daily-report-note-mobile.png') })
	await editor.getByRole('checkbox').check()
  const shutdown = editor.getByRole('button', { name: '关机时间' })
  await shutdown.click()
  picker = page.getByRole('dialog', { name: '关机时间，选择时间' })
  await expect(picker).toBeVisible()
  expect(await picker.locator('.time-picker__options').first().evaluate((element) => getComputedStyle(element).scrollbarWidth)).toBe('none')
  await expect.poll(async () => {
    const box = await picker.boundingBox()
    return box ? Math.ceil(box.y + box.height) : Number.POSITIVE_INFINITY
  }).toBeLessThanOrEqual(844)
  const panelBox = await picker.boundingBox()
  expect(panelBox).not.toBeNull()
  expect(panelBox!.x).toBeGreaterThanOrEqual(0)
  expect(panelBox!.x + panelBox!.width).toBeLessThanOrEqual(390)
  await picker.getByRole('button', { name: '22 时' }).click()
  await picker.getByRole('button', { name: '15 分' }).click()
  await page.screenshot({ path: testInfo.outputPath('time-picker-mobile.png') })
  await picker.getByRole('button', { name: '确定' }).click()
  await expect(shutdown).toContainText('22:15')
  await page.getByRole('button', { name: '保存更改' }).click()
  expect(savedConfig?.accounts[0].daily_report_time).toBe('03:45')
  expect(savedConfig?.accounts[0].stop_time).toBe('22:15')
})

test('settings tabs and webhook body remain usable at narrow widths', async ({ page, browser }, testInfo) => {
  await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
  await page.route('**/api/v1/status', (route) => route.fulfill({ json: { accounts: [], system_last_run: new Date().toISOString() } }))
  await page.route('**/api/v1/config', (route) => route.fulfill({ json: config }))

  await page.goto('/')
  await page.getByRole('button', { name: '设置', exact: true }).click()
  const tabs = page.locator('.settings-tabs')
  await page.getByRole('button', { name: '关于' }).click()
  for (const width of [900, 700, 641, 640, 390]) {
    await page.setViewportSize({ width, height: 844 })
    await expect(tabs).toBeVisible()
    const directions = await tabs.locator('button').evaluateAll((buttons) => buttons.map((button) => getComputedStyle(button).flexDirection))
    expect(directions).toEqual(Array(directions.length).fill(width <= 640 ? 'column' : 'row'))
    expect(await tabs.evaluate((element) => getComputedStyle(element).scrollbarWidth)).toBe('none')
    if (width === 641) {
      const pcScroll = await tabs.evaluate((element) => ({ width: element.clientWidth, content: element.scrollWidth }))
      expect(pcScroll.content).toBeGreaterThan(pcScroll.width)
    }
    await page.screenshot({ path: testInfo.outputPath(`settings-tabs-${width}.png`) })
  }

  const scroll = await tabs.evaluate((element) => {
    element.scrollLeft = element.scrollWidth
    return { left: element.scrollLeft, width: element.clientWidth, content: element.scrollWidth }
  })
  expect(scroll.content).toBeGreaterThan(scroll.width)
  expect(scroll.left).toBeGreaterThan(0)
  await page.getByRole('button', { name: '通知', exact: true }).click()
  await page.getByRole('button', { name: 'Webhook' }).click()
  const body = page.getByLabel('Body 模板')
  await body.fill(Array.from({ length: 30 }, (_, index) => `line ${index + 1}: #MSG#`).join('\n'))
  const bodyScroll = await body.evaluate((element) => {
    element.scrollTop = element.scrollHeight
    return element.scrollTop
  })
  expect(bodyScroll).toBeGreaterThan(0)
  await body.scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('webhook-body-mobile.png') })

  const touchContext = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
  try {
    const touchPage = await touchContext.newPage()
    await touchPage.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
    await touchPage.route('**/api/v1/status', (route) => route.fulfill({ json: { accounts: [], system_last_run: new Date().toISOString() } }))
    await touchPage.route('**/api/v1/config', (route) => route.fulfill({ json: config }))
    await touchPage.goto('/')
    await touchPage.getByRole('button', { name: '菜单' }).click()
    await touchPage.getByRole('button', { name: '设置', exact: true }).click()
    const touchTabs = touchPage.locator('.settings-tabs')
    expect(await touchTabs.evaluate((element) => matchMedia('(pointer: coarse)').matches && getComputedStyle(element).scrollbarWidth === 'none')).toBe(true)
    const touchScroll = await touchTabs.evaluate((element) => {
      element.scrollLeft = element.scrollWidth
      return element.scrollLeft
    })
    expect(touchScroll).toBeGreaterThan(0)
    await touchPage.screenshot({ path: testInfo.outputPath('settings-tabs-touch.png') })
  } finally {
    await touchContext.close()
  }
})
