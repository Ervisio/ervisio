import { Name } from '../brand';

/**
 * The Ervisio mark: a window that is also an eye, the pupil a terminal cursor (design round 033, variant a).
 * Colours come from --brand-frame / --brand-cursor (ui.css), darker on light themes.
 */
export function BrandMark({ size = 24, title }: { size?: number; title?: string }) {
  return (
    <svg className="ui-brandmark" viewBox="0 0 100 100" width={size} height={size} role={title ? 'img' : undefined} aria-hidden={title ? undefined : true} aria-label={title}>
      <rect x="8" y="22" width="84" height="56" rx="28" fill="none" stroke="var(--brand-frame)" strokeWidth="9" />
      <rect x="53" y="38" width="13" height="24" rx="3" fill="var(--brand-cursor)" />
      <rect x="31" y="47" width="16" height="6" rx="3" fill="var(--brand-frame)" opacity=".55" />
    </svg>
  );
}

/** Mark and lower-case wordmark side by side. */
export function BrandLockup({ size = 22 }: { size?: number }) {
  return (
    <span className="ui-brandlockup" style={{ fontSize: Math.round(size * 0.82) }}>
      <BrandMark size={size} />
      <span>{Name.toLowerCase()}</span>
    </span>
  );
}
