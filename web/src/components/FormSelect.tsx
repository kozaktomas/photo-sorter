import { forwardRef } from 'react';
import type { SelectHTMLAttributes } from 'react';

interface FormSelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  error?: string;
  fullWidth?: boolean;
  selectSize?: 'sm' | 'md';
}

export const FormSelect = forwardRef<HTMLSelectElement, FormSelectProps>(
  ({ className = '', label, error, fullWidth = true, selectSize = 'md', id, children, ...props }, ref) => {
    const selectId = id || (label ? label.toLowerCase().replace(/\s+/g, '-') : undefined);

    const sizeStyles = {
      sm: 'px-3 py-1.5 text-sm rounded',
      md: 'px-4 py-2 rounded-lg',
    };

    const baseStyles = `bg-slate-900 border border-slate-600 text-white focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 ${sizeStyles[selectSize]}`;
    const widthStyles = fullWidth ? 'w-full' : '';

    return (
      <div className={fullWidth ? 'w-full' : ''}>
        {label && (
          <label
            htmlFor={selectId}
            className="block text-sm font-medium text-slate-300 mb-2"
          >
            {label}
          </label>
        )}
        <select
          ref={ref}
          id={selectId}
          className={`${baseStyles} ${widthStyles} ${className}`}
          {...props}
        >
          {children}
        </select>
        {error && (
          <p className="mt-1 text-sm text-red-400">{error}</p>
        )}
      </div>
    );
  }
);

FormSelect.displayName = 'FormSelect';
