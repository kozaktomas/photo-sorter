import { forwardRef } from 'react';
import type { InputHTMLAttributes } from 'react';

interface FormInputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  fullWidth?: boolean;
  inputSize?: 'sm' | 'md';
}

export const FormInput = forwardRef<HTMLInputElement, FormInputProps>(
  ({ className = '', label, error, fullWidth = true, inputSize = 'md', id, ...props }, ref) => {
    const inputId = id ?? (label ? label.toLowerCase().replace(/\s+/g, '-') : undefined);

    const sizeStyles = {
      sm: 'px-3 py-1.5 text-sm rounded',
      md: 'px-4 py-2 rounded-lg',
    };

    const baseStyles = `bg-slate-900 border border-slate-600 text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 ${sizeStyles[inputSize]}`;
    const widthStyles = fullWidth ? 'w-full' : '';

    return (
      <div className={fullWidth ? 'w-full' : ''}>
        {label && (
          <label
            htmlFor={inputId}
            className="block text-sm font-medium text-slate-300 mb-2"
          >
            {label}
          </label>
        )}
        <input
          ref={ref}
          id={inputId}
          className={`${baseStyles} ${widthStyles} ${className}`}
          {...props}
        />
        {error && (
          <p className="mt-1 text-sm text-red-400">{error}</p>
        )}
      </div>
    );
  }
);

FormInput.displayName = 'FormInput';
