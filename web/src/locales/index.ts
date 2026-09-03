import { createI18n } from 'vue-i18n'
import en from './en'
import zh from './zh'

function getDefaultLocale(): 'zh' | 'en' {
  const saved = localStorage.getItem('locale')
  if (saved === 'zh' || saved === 'en') return saved
  // Infer from browser language
  const browserLang = navigator.language || (navigator as any).userLanguage || ''
  if (browserLang.startsWith('zh')) return 'zh'
  return 'en'
}

const savedLocale = getDefaultLocale()

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
