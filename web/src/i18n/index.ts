import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

// English translations
import enCommon from './locales/en/common.json';
import enPages from './locales/en/pages.json';
import enForms from './locales/en/forms.json';

// Czech translations
import csCommon from './locales/cs/common.json';
import csPages from './locales/cs/pages.json';
import csForms from './locales/cs/forms.json';

const resources = {
  en: {
    common: enCommon,
    pages: enPages,
    forms: enForms,
  },
  cs: {
    common: csCommon,
    pages: csPages,
    forms: csForms,
  },
};

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: 'cs',
    defaultNS: 'common',
    ns: ['common', 'pages', 'forms'],

    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: 'i18nextLng',
      caches: ['localStorage'],
    },

    interpolation: {
      escapeValue: false, // React already escapes values
    },

    // Enable Czech plural rules
    pluralSeparator: '_',

    react: {
      useSuspense: false,
    },
  });

export default i18n;
