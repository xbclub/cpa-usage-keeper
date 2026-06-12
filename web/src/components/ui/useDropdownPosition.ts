import { useCallback, useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from 'react';

const VIEWPORT_MARGIN = 8;
const DROPDOWN_OFFSET = 6;
const DROPDOWN_MAX_HEIGHT = 240;
const DROPDOWN_Z_INDEX = 2010;

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

function resolveDropdownStyle(
  anchor: HTMLElement,
  minWidth?: number,
): CSSProperties {
  const rect = anchor.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const availableWidth = Math.max(0, viewportWidth - VIEWPORT_MARGIN * 2);
  const width = Math.min(Math.max(rect.width, minWidth ?? 0), availableWidth);
  const left = clamp(
    rect.left - (width - rect.width) / 2,
    VIEWPORT_MARGIN,
    Math.max(VIEWPORT_MARGIN, viewportWidth - width - VIEWPORT_MARGIN),
  );
  const spaceBelow = viewportHeight - rect.bottom - VIEWPORT_MARGIN - DROPDOWN_OFFSET;
  const spaceAbove = rect.top - VIEWPORT_MARGIN - DROPDOWN_OFFSET;
  const direction = spaceBelow >= DROPDOWN_MAX_HEIGHT || spaceBelow >= spaceAbove ? 'down' : 'up';
  const maxHeight = Math.max(
    0,
    Math.min(DROPDOWN_MAX_HEIGHT, direction === 'down' ? spaceBelow : spaceAbove),
  );

  return direction === 'down'
    ? { position: 'fixed' as const, top: rect.bottom + DROPDOWN_OFFSET, left, width, maxHeight, zIndex: DROPDOWN_Z_INDEX }
    : { position: 'fixed' as const, bottom: viewportHeight - rect.top + DROPDOWN_OFFSET, left, width, maxHeight, zIndex: DROPDOWN_Z_INDEX };
}

/**
 * Positions a dropdown panel relative to an anchor element.
 * Tracks scroll, resize, and anchor size changes while open.
 */
export function useDropdownPosition(
  isOpen: boolean,
  anchorRef: RefObject<HTMLElement | null>,
  minWidth?: number,
): CSSProperties | null {
  const [style, setStyle] = useState<CSSProperties | null>(null);
  const rafRef = useRef<number | null>(null);

  const update = useCallback(() => {
    if (!anchorRef.current) return;
    setStyle(resolveDropdownStyle(anchorRef.current, minWidth));
  }, [anchorRef, minWidth]);

  const scheduleUpdate = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (rafRef.current !== null) {
      window.cancelAnimationFrame(rafRef.current);
    }
    rafRef.current = window.requestAnimationFrame(() => {
      rafRef.current = null;
      update();
    });
  }, [update]);

  useLayoutEffect(() => {
    if (!isOpen) {
      if (rafRef.current !== null && typeof window !== 'undefined') {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      return;
    }

    update();

    const onViewportChange = () => scheduleUpdate();

    const resizeObserver =
      typeof ResizeObserver !== 'undefined' && anchorRef.current
        ? new ResizeObserver(() => scheduleUpdate())
        : null;

    if (resizeObserver && anchorRef.current) {
      resizeObserver.observe(anchorRef.current);
    }

    window.addEventListener('resize', onViewportChange);
    window.addEventListener('scroll', onViewportChange, true);

    return () => {
      window.removeEventListener('resize', onViewportChange);
      window.removeEventListener('scroll', onViewportChange, true);
      resizeObserver?.disconnect();
      if (rafRef.current !== null) {
        window.cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [isOpen, scheduleUpdate, update, anchorRef]);

  return style;
}
