import { useT } from '../../i18n';
import type { TermHandle } from './types';

interface Props {
  ctrl: boolean;
  onCtrl(on: boolean): void;
  term(): TermHandle | null;
}

/** Keys a phone keyboard lacks. Buttons never take focus so the keyboard stays open. */
export default function ExtraKeys({ ctrl, onCtrl, term }: Props) {
  const t = useT('terminal');
  const key = (label: string, seq: string, aria?: string) => (
    <button key={label} type="button" aria-label={aria ?? label} onMouseDown={(e) => e.preventDefault()} onClick={() => term()?.sendKeys(seq)}>
      {label}
    </button>
  );
  const arrow = (label: string, dir: 'up' | 'down' | 'left' | 'right', aria: string) => (
    <button key={dir} type="button" aria-label={aria} onMouseDown={(e) => e.preventDefault()} onClick={() => term()?.arrow(dir)}>
      {label}
    </button>
  );
  return (
    <div className="terminal-keys" role="toolbar" aria-label={t('extraKeys')}>
      <button type="button" className={ctrl ? 'on' : ''} aria-pressed={ctrl} onMouseDown={(e) => e.preventDefault()} onClick={() => onCtrl(!ctrl)}>
        Ctrl
      </button>
      {key('Esc', '\x1b')}
      {key('Tab', '\t')}
      {arrow('↑', 'up', t('keyUp'))}
      {arrow('↓', 'down', t('keyDown'))}
      {arrow('←', 'left', t('keyLeft'))}
      {arrow('→', 'right', t('keyRight'))}
      {key('|', '|')}
      {key('~', '~')}
      {key('/', '/')}
      {key('-', '-')}
    </div>
  );
}
