import { forwardRef } from 'react';
import type { InputHTMLAttributes } from 'react';

interface FormCheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label: string;
}

export const FormCheckbox = forwardRef<HTMLInputElement, FormCheckboxProps>(
  ({ className = '', label, id, ...props }, ref) => {
    const inputId = id ?? label.toLowerCase().replace(/\s+/g, '-');

    return (
      <label className="flex items-center space-x-2 cursor-pointer">
        <input
          ref={ref}
          type="checkbox"
          id={inputId}
          className={`rounded bg-slate-700 border-slate-600 text-blue-500 focus:ring-blue-500 ${className}`}
          {...props}
        />
        <span className="text-slate-300">{label}</span>
      </label>
    );
  }
);

FormCheckbox.displayName = 'FormCheckbox';
