import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Loader2, AlertCircle, Inbox } from 'lucide-react';

interface LoadingStateProps {
  // State flags
  isLoading?: boolean;
  error?: string | null;
  isEmpty?: boolean;

  // Custom content
  loadingText?: string;
  emptyIcon?: ReactNode;
  emptyTitle?: string;
  emptyDescription?: string;

  // Children to render when not loading/error/empty
  children?: ReactNode;
}

export function LoadingState({
  isLoading = false,
  error = null,
  isEmpty = false,
  loadingText,
  emptyIcon,
  emptyTitle,
  emptyDescription,
  children,
}: LoadingStateProps) {
  const { t } = useTranslation('common');
  const displayLoadingText = loadingText ?? t('status.loading');
  const displayEmptyTitle = emptyTitle ?? t('status.loading');
  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-8 w-8 text-blue-500 animate-spin" />
        <span className="ml-3 text-slate-400">{displayLoadingText}</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-center py-12">
        <AlertCircle className="h-12 w-12 text-red-400 mx-auto mb-4" />
        <p className="text-red-400">{error}</p>
      </div>
    );
  }

  if (isEmpty) {
    return (
      <div className="text-center py-12 text-slate-400">
        <div className="flex justify-center mb-4">
          {emptyIcon || <Inbox className="h-12 w-12 opacity-50" />}
        </div>
        <p className="text-lg text-white mb-1">{displayEmptyTitle}</p>
        {emptyDescription && (
          <p className="text-sm">{emptyDescription}</p>
        )}
      </div>
    );
  }

  return <>{children}</>;
}

// Simpler loading spinner for inline use
export function LoadingSpinner({ size = 'md' }: { size?: 'sm' | 'md' | 'lg' }) {
  const sizeClasses = {
    sm: 'h-4 w-4',
    md: 'h-6 w-6',
    lg: 'h-8 w-8',
  };

  return <Loader2 className={`${sizeClasses[size]} text-blue-500 animate-spin`} />;
}

// Full page loading state
export function PageLoading({ text }: { text?: string }) {
  const { t } = useTranslation('common');
  const displayText = text ?? t('status.loading');

  return (
    <div className="text-center py-12 text-slate-400">
      <Loader2 className="h-8 w-8 text-blue-500 animate-spin mx-auto mb-3" />
      <p>{displayText}</p>
    </div>
  );
}
