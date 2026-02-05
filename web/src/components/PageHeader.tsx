import type { ComponentType, ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { AccentColor, Category } from '../constants/pageConfig';
import { colorMap } from '../constants/pageConfig';

interface PageHeaderProps {
  icon: ComponentType<{ className?: string }>;
  title: string;
  subtitle?: string;
  color: AccentColor;
  category?: Category;
  actions?: ReactNode;
}

export function PageHeader({ icon: Icon, title, subtitle, color, category, actions }: PageHeaderProps) {
  const { t } = useTranslation('common');
  const c = colorMap[color];

  const categoryLabel = category ? t(`categories.${category}`) : undefined;

  return (
    <div className="mb-6">
      {/* Gradient accent line */}
      <div className={`h-0.5 ${c.gradient} rounded-full mb-4`} />

      <div className="flex items-start justify-between">
        <div className="flex items-start space-x-3">
          {/* Icon */}
          <div className={`p-2 rounded-lg ${c.iconBg} mt-0.5`}>
            <Icon className={`h-5 w-5 ${c.iconText}`} />
          </div>

          <div>
            {/* Category badge */}
            {categoryLabel && (
              <span className={`inline-block text-xs font-medium px-2 py-0.5 rounded-full border mb-1 ${c.badgeBg} ${c.badgeText} ${c.badgeBorder}`}>
                {categoryLabel}
              </span>
            )}

            {/* Title */}
            <h1 className="text-2xl font-bold text-white">{title}</h1>

            {/* Subtitle */}
            {subtitle && (
              <p className="text-sm text-slate-400 mt-0.5">{subtitle}</p>
            )}
          </div>
        </div>

        {/* Actions slot */}
        {actions && (
          <div className="flex items-center space-x-2">
            {actions}
          </div>
        )}
      </div>
    </div>
  );
}
