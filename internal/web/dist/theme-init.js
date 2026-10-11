// Runs before the stylesheet and React. Kept external to respect script-src 'self'.
;(function () {
  const storageKey = 'cdt-monitor.appearance.v1'
  const defaults = { mode: 'system' }

  function normalize(value) {
    const input = value && typeof value === 'object' ? value : {}
    // Migrate the previously shipped scheduled mode to the explicit system mode.
    const mode = ['light', 'dark', 'system'].includes(input.mode) ? input.mode : defaults.mode
    return { mode }
  }

  function read() {
    try { return normalize(JSON.parse(localStorage.getItem(storageKey))) }
    catch { return { ...defaults } }
  }

  function resolve(preferences, now, systemDark) {
    if (preferences.mode === 'light' || preferences.mode === 'dark') return preferences.mode
    return systemDark ? 'dark' : 'light'
  }

  function apply(theme) {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme = theme
    const meta = document.querySelector('meta[name="theme-color"]')
    if (meta) meta.setAttribute('content', theme === 'dark' ? '#111418' : '#f1f3f5')
  }

  window.CDTTheme = { storageKey, normalize, read, resolve, apply }
  apply(resolve(read(), new Date(), window.matchMedia('(prefers-color-scheme: dark)').matches))
})()
