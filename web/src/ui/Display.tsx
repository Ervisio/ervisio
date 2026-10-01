import type { CSSProperties, ReactNode } from 'react';
import { Icon, type IconName } from './Icon';
import { Sparkline } from './Charts';

export type HueId = 'ov' | 'term' | 'file' | 'log' | 'svc' | 'sw' | 'usr' | 'plg';
export const hueClass = (h?: HueId | 'set') => (h ? `hue-${h}` : '');

/* ---------- Badge ---------- */
export type Tone = 'ok' | 'warn' | 'err' | 'info' | 'neutral';
export function Badge({ tone = 'neutral', dot, children, className }: { tone?: Tone; dot?: boolean; children: ReactNode; className?: string }) {
  const showDot = dot ?? tone !== 'neutral';
  return (
    <span className={`ui-badge ui-badge--${tone}${className ? ' ' + className : ''}`}>
      {showDot && <i />}
      {children}
    </span>
  );
}

/* ---------- Chip (filter chip, toggles aria-pressed) ---------- */
export function Chip({ pressed, onClick, icon, count, hue, children }: { pressed?: boolean; onClick?: () => void; icon?: IconName; count?: number | string; hue?: HueId; children: ReactNode }) {
  return (
    <button type="button" className={`ui-chip ${hueClass(hue)}`} aria-pressed={pressed} onClick={onClick}>
      {icon && <Icon name={icon} />}
      {children}
      {count !== undefined && <small>{count}</small>}
    </button>
  );
}

export const Kbd = ({ children }: { children: ReactNode }) => <kbd className="ui-kbd">{children}</kbd>;

/* ---------- Progress ---------- */
export function Progress({ value, tone, hue, label }: { value?: number; tone?: 'ok' | 'warn' | 'err'; hue?: HueId; label?: string }) {
  const ind = value === undefined;
  return (
    <div
      className={`ui-progress${ind ? ' ui-progress--ind' : ''}${tone ? ` ui-progress--${tone}` : ''} ${hueClass(hue)}`}
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={ind ? undefined : Math.round(value)}
    >
      <i style={ind ? undefined : { width: `${Math.max(0, Math.min(100, value))}%` }} />
    </div>
  );
}

/* ---------- Skeleton ---------- */
export function Skeleton({ width = '100%', height = 12, lines, style }: { width?: number | string; height?: number | string; lines?: number; style?: CSSProperties }) {
  if (lines) {
    return (
      <div style={{ display: 'grid', gap: 8, ...style }} aria-hidden="true">
        {Array.from({ length: lines }, (_, i) => (
          <div key={i} className="ui-skel" style={{ width: i === lines - 1 ? '55%' : '100%', height }} />
        ))}
      </div>
    );
  }
  return <div className="ui-skel" aria-hidden="true" style={{ width, height, ...style }} />;
}

/* ---------- Empty state ---------- */
export function EmptyState({ icon = 'info', title, text, action, hue }: { icon?: IconName; title: ReactNode; text?: ReactNode; action?: ReactNode; hue?: HueId }) {
  return (
    <div className={`ui-empty ${hueClass(hue)}`}>
      <span className="ui-empty-ic"><Icon name={icon} /></span>
      <b>{title}</b>
      {text && <small>{text}</small>}
      {action}
    </div>
  );
}

/* ---------- Card ---------- */
export function Card({ title, icon, action, hue, surface, children, className, style }: { title?: ReactNode; icon?: IconName; action?: ReactNode; hue?: HueId; surface?: boolean; children?: ReactNode; className?: string; style?: CSSProperties }) {
  return (
    <section className={`ui-card ${surface ? 'ui-card--surface' : ''} ${hueClass(hue)} ${className ?? ''}`} style={style}>
      {(title || action) && (
        <div className="ui-card-h">
          {icon && <span className="ui-card-ic"><Icon name={icon} /></span>}
          <h3 style={{ font: 'inherit' }}>{title}</h3>
          {action && <span className="ui-card-act">{action}</span>}
        </div>
      )}
      {children}
    </section>
  );
}

/* ---------- StatCard: tinted by section ---------- */
export interface StatCardProps {
  hue: HueId;
  icon: IconName;
  label: ReactNode;
  value: ReactNode;
  unit?: string;
  sub?: ReactNode;
  /** 0-100: shows a progress bar. */
  percent?: number;
  /** Recent values: shows a sparkline. */
  spark?: number[];
  onClick?: () => void;
}
export function StatCard({ hue, icon, label, value, unit, sub, percent, spark, onClick }: StatCardProps) {
  const body = (
    <>
      <span className="ui-stat-blob" />
      <div className="ui-stat-tp"><Icon name={icon} />{label}</div>
      {spark && <Sparkline values={spark} height={36} color="var(--h)" />}
      <div className="ui-stat-v">{value}{unit && <small>{unit}</small>}</div>
      {percent !== undefined && <Progress value={percent} label={typeof label === 'string' ? label : undefined} />}
      {sub && <div className="ui-stat-s">{sub}</div>}
    </>
  );
  return onClick ? (
    <button type="button" className={`ui-stat ${hueClass(hue)}`} style={{ textAlign: 'left', width: '100%' }} onClick={onClick}>{body}</button>
  ) : (
    <div className={`ui-stat ${hueClass(hue)}`}>{body}</div>
  );
}

/* ---------- Page: standard section container with title row ---------- */
export function Page({ title, subtitle, actions, hue, flush, children }: { title?: ReactNode; subtitle?: ReactNode; actions?: ReactNode; hue?: HueId; flush?: boolean; children: ReactNode }) {
  return (
    <div className={`ui-page ${flush ? 'ui-page--flush' : ''} ${hueClass(hue)}`}>
      {title && (
        <header className="ui-page-h">
          <div>
            <h1 className="title">{title}</h1>
            {subtitle && <p className="ui-page-sub">{subtitle}</p>}
          </div>
          {actions && <div className="ui-page-act">{actions}</div>}
        </header>
      )}
      {children}
    </div>
  );
}
