import type { ReactNode } from 'react';
import { Icon } from '../../kit';
import type { Spec } from './model';

export interface StepProps {
  spec: Spec;
  patch(p: Partial<Spec>): void;
  /** Show errors (after the user tried to go on). */
  showErrors: boolean;
}

/** A form section: a rounded card with a title and an optional small note. */
export function Section({ title, note, children }: { title: ReactNode; note?: ReactNode; children: ReactNode }) {
  return (
    <section className="dk-cr-card">
      <h3>
        {title}
        {note && <span className="dk-cr-note">{note}</span>}
      </h3>
      {children}
    </section>
  );
}

export function Hint({ tone = 'muted', icon, children }: { tone?: 'muted' | 'ok' | 'warn' | 'err'; icon?: string; children: ReactNode }) {
  return (
    <div className={`dk-cr-hint dk-cr-hint--${tone}`} role={tone === 'err' ? 'alert' : undefined}>
      {icon && <Icon name={icon} size={14} />}
      <span>{children}</span>
    </div>
  );
}

/** aria-label for kit components whose typed props do not list it. */
export const aria = (label: string): Record<string, string> => ({ 'aria-label': label });

export function replaceAt<T>(list: T[], i: number, p: Partial<T>): T[] {
  return list.map((x, j) => (j === i ? { ...x, ...p } : x));
}
export const removeAt = <T,>(list: T[], i: number): T[] => list.filter((_, j) => j !== i);
