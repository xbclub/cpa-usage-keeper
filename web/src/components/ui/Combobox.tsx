import React, {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';
import { IconChevronDown } from './icons';
import type { SelectOption } from './Select';
import { useDropdownPosition } from './useDropdownPosition';
import styles from './Combobox.module.scss';

interface ComboboxProps {
  value: string;
  options: ReadonlyArray<SelectOption>;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
  disabled?: boolean;
  ariaLabel?: string;
}

export function Combobox({
  value,
  options,
  onChange,
  placeholder,
  className,
  disabled = false,
  ariaLabel,
}: ComboboxProps) {
  const comboboxId = useId();
  const listboxId = `${comboboxId}-listbox`;
  const [open, setOpen] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const dropdownRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const isOpen = open && !disabled;

  const dropdownStyle = useDropdownPosition(isOpen, wrapRef);

  const filteredOptions = useMemo(() => {
    const query = value.trim().toLowerCase();
    if (!query) return options;
    return options.filter(
      (opt) =>
        opt.value.toLowerCase().includes(query) || opt.label.toLowerCase().includes(query),
    );
  }, [options, value]);

  useEffect(() => {
    if (!isOpen) return;
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      if (wrapRef.current?.contains(target) || dropdownRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen || highlightedIndex < 0) return;
    document.getElementById(`${comboboxId}-option-${highlightedIndex}`)
      ?.scrollIntoView({ block: 'nearest' });
  }, [isOpen, highlightedIndex, comboboxId]);

  const resolvedHighlightedIndex =
    highlightedIndex >= 0
      ? highlightedIndex
      : filteredOptions.length > 0
        ? 0
        : -1;

  const commitSelection = useCallback(
    (index: number) => {
      const opt = filteredOptions[index];
      if (!opt) return;
      onChange(opt.value);
      setOpen(false);
      setHighlightedIndex(index);
      inputRef.current?.blur();
    },
    [onChange, filteredOptions],
  );

  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      onChange(e.target.value);
      setHighlightedIndex(-1);
      if (!open) setOpen(true);
    },
    [onChange, open],
  );

  const handleFocus = useCallback(() => {
    if (!disabled) setOpen(true);
  }, [disabled]);

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLInputElement>) => {
      if (disabled) return;

      switch (event.key) {
        case 'ArrowDown':
          event.preventDefault();
          if (!isOpen) {
            setOpen(true);
            return;
          }
          if (filteredOptions.length === 0) return;
          setHighlightedIndex((prev) => (prev + 1) % filteredOptions.length);
          return;
        case 'ArrowUp':
          event.preventDefault();
          if (!isOpen) {
            setOpen(true);
            return;
          }
          if (filteredOptions.length === 0) return;
          setHighlightedIndex((prev) => (prev - 1 + filteredOptions.length) % filteredOptions.length);
          return;
        case 'Enter':
          event.preventDefault();
          if (!isOpen) {
            setOpen(true);
            return;
          }
          if (resolvedHighlightedIndex >= 0) {
            commitSelection(resolvedHighlightedIndex);
          }
          return;
        case 'Escape':
          if (!isOpen) return;
          event.preventDefault();
          setOpen(false);
          return;
        case 'Tab':
          if (isOpen) setOpen(false);
          return;
        default:
          return;
      }
    },
    [commitSelection, disabled, filteredOptions.length, isOpen, resolvedHighlightedIndex],
  );

  const handleToggle = useCallback(() => {
    if (disabled) return;
    setOpen((prev) => !prev);
    inputRef.current?.focus();
  }, [disabled]);

  const dropdown =
    isOpen && dropdownStyle
      ? createPortal(
          <div
            ref={dropdownRef}
            className={styles.dropdown}
            id={listboxId}
            role="listbox"
            aria-label={ariaLabel}
            style={dropdownStyle}
          >
            {filteredOptions.length > 0 ? (
              filteredOptions.map((opt, index) => {
                const active = opt.value === value;
                const highlighted = index === resolvedHighlightedIndex;
                return (
                  <button
                    key={opt.value}
                    id={`${comboboxId}-option-${index}`}
                    type="button"
                    role="option"
                    aria-selected={active}
                    className={`${styles.option} ${active ? styles.optionActive : ''} ${highlighted ? styles.optionHighlighted : ''}`.trim()}
                    onMouseEnter={() => setHighlightedIndex(index)}
                    onClick={() => commitSelection(index)}
                  >
                    <span className={styles.optionLabel}>{opt.label}</span>
                  </button>
                );
              })
            ) : (
              <div className={styles.noResults}>—</div>
            )}
          </div>,
          document.body,
        )
      : null;

  return (
    <>
      <div className={`${styles.wrap} ${className ?? ''}`} ref={wrapRef}>
        <div className={styles.inputWrap}>
          <input
            ref={inputRef}
            id={comboboxId}
            type="text"
            className={styles.inputField}
            value={value}
            onChange={handleInputChange}
            onFocus={handleFocus}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            disabled={disabled}
            aria-haspopup="listbox"
            aria-expanded={isOpen}
            aria-controls={isOpen ? listboxId : undefined}
            aria-activedescendant={
              isOpen && resolvedHighlightedIndex >= 0
                ? `${comboboxId}-option-${resolvedHighlightedIndex}`
                : undefined
            }
            aria-label={ariaLabel}
            autoComplete="off"
          />
          <button
            type="button"
            className={`${styles.triggerIcon} ${isOpen ? styles.triggerIconOpen : ''}`}
            onClick={handleToggle}
            tabIndex={-1}
            aria-hidden="true"
          >
            <IconChevronDown size={14} />
          </button>
        </div>
      </div>
      {dropdown}
    </>
  );
}
