import { useEffect, useState } from 'react';
import { ApiError, downloadUrl } from '../../api';
import { useI18n, useT } from '../../i18n';
import { formatBytes } from '../../lib/format';
import { Button, Checkbox, Icon, Panel, Skeleton, Tabs, toast } from '../../ui';
import { emitChanged, fcall } from './fapi';
import { KIND_ICON, canInlineImage, kindOf, looksTextual } from './kinds';
import { Thumb } from './Thumb';
import { isDirLike, type FEntry } from './types';
import { bitsToMode, dirname, formatWhen, modeBits } from './util';

export interface SidePanelProps {
  entries: FEntry[];
  admin: boolean;
  inline: boolean;
  onClose(): void;
  inTrash: boolean;
  starred: boolean;
  onEdit(e: FEntry): void;
  onDownload(e: FEntry[]): void;
  onCopyPath(e: FEntry): void;
  onTrash(e: FEntry[]): void;
  onRename(e: FEntry): void;
  onStar(e: FEntry): void;
  onChown(e: FEntry): void;
  canChown: boolean;
}

type Tab = 'details' | 'preview';

export function SidePanel(p: SidePanelProps) {
  const t = useT('files');
  const [tab, setTab] = useState<Tab>('details');
  const e = p.entries.length === 1 ? p.entries[0] : null;
  const k = e ? kindOf(e) : 'other';
  useEffect(() => {
    if (e && isDirLike(e)) setTab('details');
  }, [e?.path]); // eslint-disable-line react-hooks/exhaustive-deps

  const many = p.entries.length > 1;
  const title = e ? e.name : t('selectedCount', { count: p.entries.length });
  return (
    <Panel
      inline={p.inline}
      open
      onClose={p.onClose}
      title={title}
      subtitle={e ? typeLabel(e, t) : formatBytes(p.entries.reduce((s, x) => s + (isDirLike(x) ? 0 : x.size), 0))}
      icon={e ? KIND_ICON[k] : 'files'}
      hue="file"
      tabs={e && !isDirLike(e) ? <Tabs<Tab> variant="pill" value={tab} onChange={setTab} items={[{ id: 'details', label: t('tab.details') }, { id: 'preview', label: t('tab.preview') }]} /> : undefined}
    >
      {many && <Summary entries={p.entries} p={p} t={t} />}
      {e && tab === 'details' && <Details e={e} p={p} t={t} />}
      {e && tab === 'preview' && <Preview e={e} p={p} t={t} />}
    </Panel>
  );
}

function typeLabel(e: FEntry, t: ReturnType<typeof useT>) {
  if (e.type === 'dir') return t('type.folder');
  if (e.type === 'symlink') return t('type.link');
  if (e.type === 'other') return t('type.special');
  return e.mime || t(`kind.${kindOf(e)}`);
}

function Summary({ entries, p, t }: { entries: FEntry[]; p: SidePanelProps; t: ReturnType<typeof useT> }) {
  const folders = entries.filter(isDirLike).length;
  return (
    <div className="files-side">
      <dl className="files-kv">
        <dt>{t('summary.folders')}</dt>
        <dd>{folders}</dd>
        <dt>{t('summary.files')}</dt>
        <dd>{entries.length - folders}</dd>
        <dt>{t('col.size')}</dt>
        <dd>{formatBytes(entries.reduce((s, x) => s + (isDirLike(x) ? 0 : x.size), 0))}</dd>
      </dl>
      <div className="files-acts">
        {!p.inTrash && entries.every((x) => !isDirLike(x)) && (
          <button type="button" onClick={() => p.onDownload(entries)}>
            <Icon name="download" />
            {t('act.download')}
          </button>
        )}
        <button type="button" className="is-danger" onClick={() => p.onTrash(entries)}>
          <Icon name="trash" />
          {p.inTrash ? t('act.deleteForever') : t('act.trash')}
        </button>
      </div>
    </div>
  );
}

function Details({ e, p, t }: { e: FEntry; p: SidePanelProps; t: ReturnType<typeof useT> }) {
  const { lang } = useI18n();
  const dir = isDirLike(e);
  return (
    <div className="files-side">
      <div className="files-pv">{dir ? <span className="files-ic files-ic--big files-t-dir"><Icon name="files" /></span> : <Thumb e={e} admin={p.admin} />}</div>
      <dl className="files-kv">
        <dt>{t('col.type')}</dt>
        <dd>{typeLabel(e, t)}</dd>
        {!dir && (
          <>
            <dt>{t('col.size')}</dt>
            <dd>{formatBytes(e.size)}</dd>
          </>
        )}
        <dt>{p.inTrash ? t('col.deleted') : t('col.modified')}</dt>
        <dd>{formatWhen(p.inTrash && e.deletedAt ? e.deletedAt : e.mtime, lang)}</dd>
        <dt>{t('col.owner')}</dt>
        <dd>
          {e.owner} : {e.group}
        </dd>
        <dt>{p.inTrash ? t('originalPlace') : t('col.path')}</dt>
        <dd className="mono" title={p.inTrash ? e.origin : e.path}>
          {p.inTrash ? e.origin || '-' : e.path}
        </dd>
        {e.type === 'symlink' && e.target && (
          <>
            <dt>{t('linkTo')}</dt>
            <dd className="mono">{e.target}</dd>
          </>
        )}
      </dl>
      {!p.inTrash && e.type !== 'symlink' && <Permissions e={e} admin={p.admin} t={t} />}
      <div className="files-acts">
        {!dir && !p.inTrash && (
          <button type="button" onClick={() => p.onDownload([e])}>
            <Icon name="download" />
            {t('act.download')}
          </button>
        )}
        {!p.inTrash && (
          <button type="button" onClick={() => p.onCopyPath(e)}>
            <Icon name="copy" />
            {t('act.copyPath')}
          </button>
        )}
        {!p.inTrash && (
          <button type="button" onClick={() => p.onRename(e)}>
            <Icon name="edit" />
            {t('act.rename')}
          </button>
        )}
        {!p.inTrash && (
          <button type="button" onClick={() => p.onStar(e)}>
            <Icon name="star" />
            {p.starred ? t('act.unstar') : t('act.star')}
          </button>
        )}
        {p.canChown && !p.inTrash && (
          <button type="button" onClick={() => p.onChown(e)}>
            <Icon name="users" />
            {t('act.chown')}
          </button>
        )}
        <button type="button" className="is-danger" onClick={() => p.onTrash([e])}>
          <Icon name="trash" />
          {p.inTrash ? t('act.deleteForever') : t('act.trash')}
        </button>
      </div>
    </div>
  );
}

const ROWS = ['owner', 'group', 'others'] as const;
const COLS = ['read', 'write', 'run'] as const;

function Permissions({ e, admin, t }: { e: FEntry; admin: boolean; t: ReturnType<typeof useT> }) {
  const [bits, setBits] = useState(() => modeBits(e.mode));
  const [mode, setMode] = useState(e.mode);
  const [rec, setRec] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setBits(modeBits(e.mode));
    setMode(e.mode);
    setRec(false);
  }, [e.path, e.mode]);
  const dir = e.type === 'dir';

  const toggle = async (i: number) => {
    const next = bits.map((b, j) => (j === i ? !b : b));
    const m = bitsToMode(next, e.mode);
    const prev = bits;
    setBits(next);
    setMode(m);
    setBusy(true);
    try {
      await fcall('files.chmod', { path: e.path, mode: m, recursive: rec && dir }, { admin });
      emitChanged(dirname(e.path));
    } catch (x) {
      setBits(prev);
      setMode(e.mode);
      toast.err(t('err.chmod'), x instanceof ApiError ? x.message : undefined);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div>
      <h4 className="files-h4">
        {t('permissions')} <span className="mono">{mode.replace(/^0/, '')}</span>
      </h4>
      <div className="files-perm" aria-busy={busy}>
        <span />
        {COLS.map((c) => (
          <span key={c} className="files-perm-h">
            {t(`perm.${c}`)}
          </span>
        ))}
        {ROWS.map((r, ri) => (
          <PermRow key={r} label={t(`perm.${r}`)} bits={bits.slice(ri * 3, ri * 3 + 3)} onToggle={(ci) => toggle(ri * 3 + ci)} cols={COLS.map((c) => t(`perm.${c}`))} />
        ))}
      </div>
      {dir && (
        <div className="files-perm-rec">
          <Checkbox checked={rec} onChange={setRec} label={t('permRecursive')} />
        </div>
      )}
    </div>
  );
}

function PermRow({ label, bits, onToggle, cols }: { label: string; bits: boolean[]; onToggle(i: number): void; cols: string[] }) {
  return (
    <>
      <span className="files-perm-r">{label}</span>
      {bits.map((on, i) => (
        <button key={i} type="button" className={`files-ck${on ? ' on' : ''}`} role="switch" aria-checked={on} aria-label={`${label}: ${cols[i]}`} onClick={() => onToggle(i)}>
          {on && <Icon name="check" />}
        </button>
      ))}
    </>
  );
}

function Preview({ e, p, t }: { e: FEntry; p: SidePanelProps; t: ReturnType<typeof useT> }) {
  const k = kindOf(e);
  const [text, setText] = useState<{ content: string; truncated: boolean; encoding: string } | null | 'binary'>(null);
  const [err, setErr] = useState('');
  const textual = looksTextual(e);
  useEffect(() => {
    setText(null);
    setErr('');
    if (!textual || !e.path) return;
    let live = true;
    fcall<{ content: string; truncated: boolean; encoding: string }>('files.readText', { path: e.path, maxBytes: 64 << 10 }, { admin: p.admin }).then(
      (r) => live && setText(r),
      (x) => {
        if (!live) return;
        if (x instanceof ApiError && x.code === 'invalid') setText('binary');
        else setErr(x instanceof ApiError ? x.message : String(x));
      },
    );
    return () => {
      live = false;
    };
  }, [e.path, e.mtime, textual, p.admin]);

  if (k === 'img' && canInlineImage(e.name)) {
    return (
      <div className="files-side">
        <div className="files-pv files-pv--img">
          <img src={downloadUrl(e.path, p.admin, true)} alt={e.name} />
        </div>
      </div>
    );
  }
  if (textual) {
    return (
      <div className="files-side">
        {err ? (
          <p className="files-muted">{err}</p>
        ) : text === null ? (
          <Skeleton lines={6} height={12} />
        ) : text === 'binary' ? (
          <p className="files-muted">{t('preview.binary')}</p>
        ) : (
          <>
            <pre className="files-pre">{text.content || t('preview.emptyFile')}</pre>
            {text.truncated && <p className="files-muted">{t('preview.truncated')}</p>}
            {!p.inTrash && !text.truncated && (
              <Button icon="edit" onClick={() => p.onEdit(e)}>
                {t('act.edit')}
              </Button>
            )}
            <dl className="files-kv">
              <dt>{t('encoding')}</dt>
              <dd>{text.encoding}</dd>
            </dl>
          </>
        )}
      </div>
    );
  }
  return (
    <div className="files-side">
      <p className="files-muted">{t('preview.none')}</p>
    </div>
  );
}
