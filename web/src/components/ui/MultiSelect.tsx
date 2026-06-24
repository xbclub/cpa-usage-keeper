import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { IconChevronDown } from './icons';
import { useDropdownPosition } from './useDropdownPosition';
import styles from './MultiSelect.module.scss';

export interface MultiSelectOption {
  value: string;
  label: string;
}

interface MultiSelectProps {
  value: string[];
  options: ReadonlyArray<MultiSelectOption>;
  onChange: (value: string[]) => void;
  /** 选中 N 个时的触发器文案模板，{{count}} 会被替换成数量 */
  selectedLabel: (count: number) => string;
  /** 未选中时显示的占位文案 */
  placeholder?: string;
  className?: string;
  disabled?: boolean;
  ariaLabel?: string;
  fullWidth?: boolean;
  dropdownMinWidth?: number;
}

/**
 * MultiSelect 是带 checkbox 的多选下拉。复用 useDropdownPosition 定位和 _dropdown-panel.scss 下拉面板样式。
 * 触发器关闭状态显示「已选 N 个模型」（由 selectedLabel 决定），未选中显示 placeholder。
 */
export function MultiSelect({
  value,
  options,
  onChange,
  selectedLabel,
  placeholder,
  className,
  disabled,
  ariaLabel,
  fullWidth,
  dropdownMinWidth,
}: MultiSelectProps) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const dropdownId = useId();
  const isOpen = open && !disabled;
  const dropdownStyle = useDropdownPosition(isOpen, triggerRef, dropdownMinWidth);

  const selectedSet = useMemo(() => new Set(value), [value]);

  // 点击外部关闭
  useEffect(() => {
    if (!isOpen) return;
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      if (triggerRef.current?.contains(target)) return;
      const dropdown = document.getElementById(dropdownId);
      if (dropdown?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [isOpen, dropdownId]);

  const toggleOption = useCallback(
    (optionValue: string) => {
      if (selectedSet.has(optionValue)) {
        onChange(value.filter((v) => v !== optionValue));
      } else {
        onChange([...value, optionValue]);
      }
    },
    [onChange, selectedSet, value],
  );

  const displayText = value.length > 0 ? selectedLabel(value.length) : (placeholder ?? '');

  return (
    <div className={[styles.wrap, fullWidth ? styles.fullWidth : '', className ?? ''].join(' ').trim()}>
      <button
        ref={triggerRef}
        type="button"
        className={styles.trigger}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        aria-label={ariaLabel}
        onClick={() => setOpen((prev) => !prev)}
      >
        <span className={styles.triggerText}>{displayText}</span>
        <span className={styles.triggerIcon} aria-hidden="true">
          <IconChevronDown />
        </span>
      </button>
      {isOpen && dropdownStyle && createPortal(
        <div id={dropdownId} className={styles.dropdown} style={dropdownStyle} role="listbox" aria-multiselectable="true">
          {options.map((option) => {
            const checked = selectedSet.has(option.value);
            return (
              <button
                key={option.value}
                type="button"
                role="option"
                aria-selected={checked}
                className={[styles.option, checked ? styles.optionActive : ''].join(' ').trim()}
                onClick={() => toggleOption(option.value)}
              >
                <span className={styles.checkbox} aria-hidden="true">
                  {checked ? '✓' : ''}
                </span>
                <span className={styles.optionLabel} title={option.label}>
                  {option.label}
                </span>
              </button>
            );
          })}
        </div>,
        document.body,
      )}
    </div>
  );
}
