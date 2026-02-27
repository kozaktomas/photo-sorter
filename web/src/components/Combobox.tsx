import { useState, useRef, useEffect, useCallback, useId } from 'react';
import { ChevronDown, X } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

export interface ComboboxOption {
  value: string;
  label: string;
}

interface ComboboxProps {
  value: string;
  onChange: (value: string) => void;
  options: ComboboxOption[];
  placeholder?: string;
  icon?: LucideIcon;
  size?: 'sm' | 'md';
  className?: string;
  focusRingClass?: string;
}

export function Combobox({
  value,
  onChange,
  options,
  placeholder = '',
  icon: Icon,
  size = 'md',
  className = '',
  focusRingClass,
}: ComboboxProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [highlightIndex, setHighlightIndex] = useState(-1);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const id = useId();
  const listboxId = `${id}-listbox`;

  const selectedOption = options.find((o) => o.value === value);

  const filtered = query
    ? options.filter((o) => o.label.toLowerCase().includes(query.toLowerCase()))
    : options;

  const open = useCallback(() => {
    setIsOpen(true);
    setQuery('');
    setHighlightIndex(-1);
  }, []);

  const close = useCallback(() => {
    setIsOpen(false);
    setQuery('');
    setHighlightIndex(-1);
  }, []);

  const select = useCallback(
    (val: string) => {
      onChange(val);
      close();
    },
    [onChange, close],
  );

  // Click-outside
  useEffect(() => {
    if (!isOpen) return;
    const handleMouseDown = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        close();
      }
    };
    document.addEventListener('mousedown', handleMouseDown);
    return () => document.removeEventListener('mousedown', handleMouseDown);
  }, [isOpen, close]);

  // Scroll highlighted item into view
  useEffect(() => {
    if (highlightIndex < 0 || !listRef.current) return;
    const item = listRef.current.children[highlightIndex] as HTMLElement | undefined;
    item?.scrollIntoView({ block: 'nearest' });
  }, [highlightIndex]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!isOpen) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') {
        e.preventDefault();
        open();
      }
      return;
    }

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setHighlightIndex((prev) => (prev < filtered.length - 1 ? prev + 1 : 0));
        break;
      case 'ArrowUp':
        e.preventDefault();
        setHighlightIndex((prev) => (prev > 0 ? prev - 1 : filtered.length - 1));
        break;
      case 'Enter':
        e.preventDefault();
        if (highlightIndex >= 0 && highlightIndex < filtered.length) {
          select(filtered[highlightIndex].value);
        }
        break;
      case 'Escape':
        e.preventDefault();
        close();
        break;
      case 'Tab':
        close();
        break;
    }
  };

  const isSm = size === 'sm';
  const ringClass = focusRingClass ?? (isSm
    ? 'focus-within:ring-1 focus-within:ring-rose-500'
    : 'focus-within:ring-2 focus-within:ring-blue-500');

  const activeDescendant =
    highlightIndex >= 0 && highlightIndex < filtered.length
      ? `${id}-option-${highlightIndex}`
      : undefined;

  return (
    <div ref={containerRef} className={`relative ${className}`}>
      <div
        className={`flex items-center bg-slate-800 border border-slate-600 ${
          isSm ? 'rounded text-sm' : 'rounded-lg'
        } ${ringClass}`}
      >
        {Icon && (
          <Icon className={`${isSm ? 'ml-2 h-4 w-4' : 'ml-3 h-5 w-5'} text-slate-400 shrink-0`} />
        )}
        <input
          ref={inputRef}
          role="combobox"
          aria-expanded={isOpen}
          aria-controls={listboxId}
          aria-activedescendant={activeDescendant}
          aria-autocomplete="list"
          type="text"
          value={isOpen ? query : selectedOption?.label ?? ''}
          placeholder={placeholder}
          onChange={(e) => {
            setQuery(e.target.value);
            setHighlightIndex(-1);
            if (!isOpen) open();
          }}
          onFocus={() => {
            if (!isOpen) open();
          }}
          onClick={() => {
            if (!isOpen) open();
          }}
          onKeyDown={handleKeyDown}
          className={`flex-1 min-w-0 bg-transparent text-white placeholder-slate-500 focus:outline-none ${
            isSm ? 'px-2 py-1.5' : `${Icon ? 'pl-2' : 'pl-4'} pr-2 py-2`
          }`}
        />
        {value ? (
          <button
            type="button"
            tabIndex={-1}
            onClick={(e) => {
              e.stopPropagation();
              onChange('');
              close();
              inputRef.current?.focus();
            }}
            className={`${isSm ? 'mr-1.5' : 'mr-2'} text-slate-400 hover:text-white shrink-0`}
            aria-label="Clear"
          >
            <X className={isSm ? 'h-3.5 w-3.5' : 'h-4 w-4'} />
          </button>
        ) : (
          <ChevronDown
            className={`${isSm ? 'mr-1.5 h-3.5 w-3.5' : 'mr-2 h-4 w-4'} text-slate-400 pointer-events-none shrink-0`}
          />
        )}
      </div>

      {isOpen && (
        <ul
          ref={listRef}
          id={listboxId}
          role="listbox"
          className="absolute left-0 right-0 top-full mt-1 max-h-60 overflow-y-auto bg-slate-800 border border-slate-600 rounded-lg shadow-xl z-50"
        >
          {filtered.length === 0 ? (
            <li className={`${isSm ? 'px-2 py-1.5 text-sm' : 'px-4 py-2'} text-slate-500`}>
              No matches
            </li>
          ) : (
            filtered.map((option, i) => (
              <li
                key={option.value}
                id={`${id}-option-${i}`}
                role="option"
                aria-selected={option.value === value}
                onMouseDown={(e) => {
                  e.preventDefault();
                  select(option.value);
                }}
                onMouseEnter={() => setHighlightIndex(i)}
                className={`cursor-pointer ${isSm ? 'px-2 py-1.5 text-sm' : 'px-4 py-2'} ${
                  i === highlightIndex
                    ? 'bg-slate-700 text-white'
                    : option.value === value
                      ? 'text-blue-400'
                      : 'text-slate-300 hover:bg-slate-700/50'
                }`}
              >
                {option.label}
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  );
}
