import { useEffect, useRef, useState } from 'react';
import { errorMessage } from '../../api';
import { useT } from '../../i18n';
import { validFileName } from './util';
import './inline-name.css';

export function InlineName({
  name,
  folder = false,
  onSubmit,
  onCancel,
}: {
  name: string;
  folder?: boolean;
  onSubmit(name: string): Promise<void>;
  onCancel(): void;
}) {
  const t = useT('files');
  const ref = useRef<HTMLInputElement>(null);
  const working = useRef(false);
  const done = useRef(false);
  const [value, setValue] = useState(name);
  const [error, setError] = useState('');
  useEffect(() => {
    const input = ref.current;
    input?.focus();
    const dot = name.lastIndexOf('.');
    input?.setSelectionRange(0, !folder && dot > 0 ? dot : name.length);
    const parent = input?.closest<HTMLElement>('[data-desktop-item],.files-pn');
    return () => {
      if (
        done.current &&
        (document.activeElement === input || document.activeElement === document.body)
      )
        window.requestAnimationFrame(
          () => parent?.isConnected && parent.focus({ preventScroll: true }),
        );
    };
  }, [name, folder]);
  const commit = async () => {
    if (working.current || done.current) return;
    const next = value.trim();
    if (!validFileName(next)) {
      setError(t('err.badName'));
      ref.current?.focus();
      return;
    }
    working.current = true;
    done.current = true;
    try {
      await onSubmit(next);
      done.current = true;
    } catch (e) {
      done.current = false;
      setError(errorMessage(e));
      ref.current?.focus();
    } finally {
      working.current = false;
    }
  };
  return (
    <span
      className="files-inline-name"
      onClick={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      onMouseDown={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
      onContextMenu={(e) => e.stopPropagation()}
      draggable={false}
    >
      <input
        ref={ref}
        aria-label={t('act.rename')}
        aria-invalid={!!error}
        value={value}
        spellCheck={false}
        onChange={(e) => {
          setValue(e.target.value);
          setError('');
        }}
        onBlur={() => void commit()}
        onKeyDown={(e) => {
          e.stopPropagation();
          if (e.key === 'Enter') {
            e.preventDefault();
            void commit();
          }
          if (e.key === 'Escape') {
            e.preventDefault();
            done.current = true;
            onCancel();
          }
        }}
      />
      {error && (
        <span role="alert" className="files-inline-error">
          {error}
        </span>
      )}
    </span>
  );
}
