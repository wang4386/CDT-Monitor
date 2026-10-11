import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { defaultConfig } from '../src/types'

const storageKey = 'cdt-monitor.appearance.v1'

async function dashboard(page: Page) {
  await page.route('**/api/v1/system/init-status', (route) => route.fulfill({ json: { initialized: true } }))
  await page.route('**/api/v1/config', (route) => route.fulfill({ json: defaultConfig() }))
  await page.route('**/api/v1/status', (route) => route.fulfill({ json: {
    accounts: [{ id: 1, account: 'DEMO***', remark: '预览节点', region: 'cn-hongkong', region_name: '中国香港', flow_total: 200, flow_used: 68.4, percentage: 34.2, threshold: 95, over_threshold: false, instance_status: 'Running', last_updated: new Date().toISOString(), stale: false }],
    system_last_run: new Date().toISOString(),
  } }))
}

async function chooseTheme(page: Page, label: string) {
  await page.getByRole('button', { name: /外观：/ }).click()
  await page.getByRole('menuitemradio', { name: label }).click()
}

test('system preference updates live, manual overrides persist, tabs synchronize', async ({ page, context }) => {
  await dashboard(page)
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.goto('/')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await page.emulateMedia({ colorScheme: 'light' })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await chooseTheme(page, '暗色')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.getByRole('menuitemradio', { name: '按时间', exact: true })).toHaveCount(0)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

  const second = await context.newPage()
  await dashboard(second)
  await second.goto('/')
  await expect(second.locator('html')).toHaveAttribute('data-theme', 'dark')
  await chooseTheme(page, '浅色')
  await expect(second.locator('html')).toHaveAttribute('data-theme', 'light')
  await second.evaluate((key) => localStorage.removeItem(key), storageKey)
  await expect(page.getByRole('button', { name: '外观：跟随系统' })).toHaveAttribute('aria-label', '外观：跟随系统')
  await second.close()
})

test('legacy scheduled preference migrates to system and unavailable storage remains usable', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.addInitScript(({ key }) => { localStorage.setItem(key, JSON.stringify({ mode: 'schedule', lightTime: '07:00', darkTime: '19:00' })) }, { key: storageKey })
  await dashboard(page)
  await page.goto('/')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.getByRole('button', { name: '外观：跟随系统' })).toBeVisible()
  await page.addInitScript(() => {
    Object.defineProperty(Storage.prototype, 'getItem', { value: () => { throw new Error('disabled') } })
    Object.defineProperty(Storage.prototype, 'setItem', { value: () => { throw new Error('disabled') } })
  })
  await page.goto('/')
  await chooseTheme(page, '浅色')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
})

test('desktop theme menu stays above content and mobile menu remains within the viewport', async ({ page }) => {
  await dashboard(page)
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.goto('/')
  const metric = page.locator('.metric-strip')
  await page.getByRole('button', { name: '外观：跟随系统' }).click()
  const menu = page.locator('.theme-menu__popover')
  const menuBox = await menu.boundingBox()
  const metricBox = await metric.boundingBox()
  expect(menuBox).not.toBeNull()
  expect(metricBox).not.toBeNull()
  expect(menuBox!.y + menuBox!.height).toBeGreaterThan(metricBox!.y)
  const overlapLeft = Math.max(menuBox!.x, metricBox!.x)
  const overlapRight = Math.min(menuBox!.x + menuBox!.width, metricBox!.x + metricBox!.width)
  const overlapTop = Math.max(menuBox!.y, metricBox!.y)
  const overlapBottom = Math.min(menuBox!.y + menuBox!.height, metricBox!.y + metricBox!.height)
  const overlapX = (overlapLeft + overlapRight) / 2
  const overlapY = (overlapTop + overlapBottom) / 2
  expect(overlapRight).toBeGreaterThan(overlapLeft)
  expect(overlapBottom).toBeGreaterThan(overlapTop)
  expect(await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.closest('.theme-menu__popover') !== null, { x: overlapX, y: overlapY })).toBe(true)
  await expect(menu.getByRole('menuitemradio')).toHaveCount(3)
  await page.screenshot({ path: 'test-results/theme-menu-desktop.png', fullPage: true })
  await page.getByRole('menuitemradio', { name: '暗色' }).click()

  await page.setViewportSize({ width: 320, height: 720 })
  await page.getByRole('button', { name: '菜单', exact: true }).click()
  await page.getByRole('button', { name: '外观：暗色', exact: true }).click()
  const mobileMenu = page.locator('.theme-menu__popover')
  const mobileBox = await mobileMenu.boundingBox()
  expect(mobileBox!.x).toBeGreaterThanOrEqual(0)
  expect(mobileBox!.x + mobileBox!.width).toBeLessThanOrEqual(320)
  await page.screenshot({ path: 'test-results/theme-menu-mobile.png', fullPage: true })
})
