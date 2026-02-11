import { useTranslation } from 'react-i18next';
import { Globe } from 'lucide-react';

export function LanguageSwitcher() {
  const { i18n } = useTranslation();

  const toggleLanguage = () => {
    const newLang = i18n.language === 'cs' ? 'en' : 'cs';
    void i18n.changeLanguage(newLang);
  };

  const currentFlag = i18n.language === 'cs' ? '🇨🇿' : '🇬🇧';
  const currentLabel = i18n.language === 'cs' ? 'CZ' : 'EN';

  return (
    <button
      onClick={toggleLanguage}
      className="flex items-center space-x-1.5 px-3 py-2 rounded-md text-sm font-medium text-slate-300 hover:bg-slate-700 hover:text-white transition-colors"
      title={i18n.language === 'cs' ? 'Switch to English' : 'Přepnout do češtiny'}
    >
      <Globe className="h-4 w-4" />
      <span>{currentFlag}</span>
      <span>{currentLabel}</span>
    </button>
  );
}
