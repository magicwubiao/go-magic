import { createI18n } from 'vue-i18n'
import en from './en'
import zh from './zh'

function getDefaultLocale(): 'zh' | 'en' {
  // English is the default for the product. Only honor an explicit user choice
  // (persisted via the language switch); do NOT auto-follow the browser locale,
  // otherwise a Chinese browser would silently start in Chinese on first visit.
  const saved = localStorage.getItem('locale')
  if (saved === 'zh' || saved === 'en') return saved
  return 'en'
}

const savedLocale = getDefaultLocale()

// Reflect the resolved locale on <html lang> from the very first paint, so
// assistive tech and `:lang()` styles match what is actually rendered.
if (typeof document !== 'undefined') {
  document.documentElement.setAttribute('lang', savedLocale)
}

export const i18n = createI18n({
  legacy: false,
  locale: savedLocale,
  fallbackLocale: 'en',
  messages: {
    en,
    zh
  }
})

export const locales = [
  { code: 'en', name: 'English' },
  { code: 'zh', name: '中文' }
]

export function setLocale(locale: string) {
  i18n.global.locale.value = locale as 'en' | 'zh'
  localStorage.setItem('locale', locale)
  document.documentElement.setAttribute('lang', locale)
}

export function getLocale(): string {
  return i18n.global.locale.value
}
