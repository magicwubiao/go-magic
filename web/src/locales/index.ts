import { createI18n } from 'vue-i18n'
import en from './en'
import zh from './zh'

const savedLocale = localStorage.getItem('locale') || 'zh'

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
