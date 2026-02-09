import type { ReactNode } from 'react';

const variantStyles = {
  error: 'p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm',
  success: 'p-3 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm',
  warning: 'p-4 bg-yellow-500/10 border border-yellow-500/20 rounded-lg text-yellow-400',
};

interface AlertProps {
  variant: 'error' | 'success' | 'warning';
  children: ReactNode;
  className?: string;
}

export function Alert({ variant, children, className }: AlertProps) {
  return (
    <div className={`${variantStyles[variant]}${className ? ` ${className}` : ''}`}>
      {children}
    </div>
  );
}
