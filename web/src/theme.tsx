import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useState } from 'react'
import type { ReactNode } from 'react'

export type ThemeMode = 'light' | 'dark' | 'system'
type Theme = 'light' | 'dark'
type Preferences = { mode: ThemeMode }

declare global {
  interface Window {
    CDTTheme: {
      storageKey: string
      normalize: (value: unknown) => Preferences
      read: () => Preferences
      resolve: (preferences: Preferences, now: Date, systemDark: boolean) => Theme
      apply: (theme: Theme) => void
    }
  }
}

type ThemeContextValue = {
  preferences: Preferences
  theme: Theme
  persistenceAvailable: boolean
  updatePreferences: (patch: Partial<Preferences>) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preferences, setPreferences] = useState(() => window.CDTTheme.read())
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  const [persistenceAvailable, setPersistenceAvailable] = useState(true)
  const theme = window.CDTTheme.resolve(preferences, new Date(), systemDark)

  useLayoutEffect(() => { window.CDTTheme.apply(theme) }, [theme])

  const updatePreferences = useCallback((patch: Partial<Preferences>) => {
    const next = window.CDTTheme.normalize({ ...preferences, ...patch })
    setPreferences(next)
    try {
      localStorage.setItem(window.CDTTheme.storageKey, JSON.stringify(next))
      setPersistenceAvailable(true)
    } catch { setPersistenceAvailable(false) }
  }, [preferences])

  useEffect(() => {
    try {
      localStorage.getItem(window.CDTTheme.storageKey)
      setPersistenceAvailable(true)
    } catch { setPersistenceAvailable(false) }
  }, [])

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const syncSystem = () => setSystemDark(media.matches)
    const syncClock = () => syncSystem()
    const syncStorage = (event: StorageEvent) => {
      if (event.key === window.CDTTheme.storageKey || event.key === null) {
        setPreferences(window.CDTTheme.read())
        syncClock()
      }
    }
    const onVisible = () => { if (document.visibilityState === 'visible') syncClock() }
    media.addEventListener('change', syncSystem)
    window.addEventListener('storage', syncStorage)
    window.addEventListener('focus', syncClock)
    window.addEventListener('pageshow', syncClock)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      media.removeEventListener('change', syncSystem)
      window.removeEventListener('storage', syncStorage)
      window.removeEventListener('focus', syncClock)
      window.removeEventListener('pageshow', syncClock)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [])

  return <ThemeContext.Provider value={{ preferences, theme, persistenceAvailable, updatePreferences }}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const context = useContext(ThemeContext)
  if (!context) throw new Error('ThemeProvider is required')
  return context
}
