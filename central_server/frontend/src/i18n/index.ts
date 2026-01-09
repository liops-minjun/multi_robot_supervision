import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { ko } from './ko'
import { en } from './en'

export type Language = 'ko' | 'en'

export type TranslationKey = keyof typeof ko

interface I18nState {
  language: Language
  setLanguage: (lang: Language) => void
  t: (key: TranslationKey, params?: Record<string, string | number>) => string
}

const translations = { ko, en }

export const useI18n = create<I18nState>()(
  persist(
    (set, get) => ({
      language: 'ko', // 기본값 한국어

      setLanguage: (lang: Language) => set({ language: lang }),

      t: (key: TranslationKey, params?: Record<string, string | number>) => {
        const { language } = get()
        let text: string = translations[language][key] || translations['en'][key] || key

        // 파라미터 치환 {{param}}
        if (params) {
          Object.entries(params).forEach(([k, v]) => {
            text = text.replace(new RegExp(`{{${k}}}`, 'g'), String(v))
          })
        }

        return text
      },
    }),
    {
      name: 'fleet-language',
    }
  )
)

// 편의 훅
export const useTranslation = () => {
  const { t, language, setLanguage } = useI18n()
  return { t, language, setLanguage }
}
