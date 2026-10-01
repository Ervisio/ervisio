import { useEffect, useRef, useState } from 'react';
import { ApiError } from '../../api';
import { useT } from '../../i18n';
import { Badge, Button, Checkbox, Dialog, Icon, Input, Textarea, toast } from '../../ui';
import { emitChanged, fcall } from './fapi';
import type { FEntry } from './types';
import { dirname, validFileName } from './util';

/* ---------- name prompt: new folder, new file, rename ---------- */
export function NameDialog({ title, label, initial, confirm, onClose, onSubmit }: { title: string; label: string; initial: string; confirm: string; onClose(): void; onSubmit(name: string): Promise<void> }) {
  const t = useT('files');
  const [val, setVal] = useState(initial);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.focus();
    const dot = initial.lastIndexOf('.');
    el.setSelectionRange(0, dot > 0 ? dot : initial.length);
  }, [initial]);
  const submit = async () => {
    const name = val.trim();
    if (!validFileName(name)) return setErr(t('err.badName'));
    setBusy(true);
    setErr('');
    try {
      await onSubmit(name);
      onClose();
    } catch (x) {
      setErr(x instanceof ApiError ? (x.code === 'conflict' ? t('err.exists', { name }) : x.message) : String(x));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onClose={onClose}
      title={title}
      icon="files"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button type="submit" variant="primary" loading={busy}>
            {confirm}
          </Button>
        </>
      }
    >
      <Input ref={ref} label={label} value={val} onChange={(e) => setVal(e.target.value)} error={err || undefined} autoComplete="off" spellCheck={false} />
    </Dialog>
  );
}

/* ---------- owner / group (administrator) ---------- */
export function ChownDialog({ entry, onClose }: { entry: FEntry; onClose(): void }) {
  const t = useT('files');
  const [owner, setOwner] = useState(entry.owner);
  const [group, setGroup] = useState(entry.group);
  const [rec, setRec] = useState(false);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('chown.title', { name: entry.name })}
      icon="users"
      onSubmit={async (ev) => {
        ev.preventDefault();
        setBusy(true);
        setErr('');
        try {
          await fcall('files.chown', { path: entry.path, owner: owner === entry.owner ? '' : owner, group: group === entry.group ? '' : group, recursive: rec });
          toast.ok(t('chown.done', { name: entry.name }));
          emitChanged(dirname(entry.path));
          onClose();
        } catch (x) {
          setErr(x instanceof ApiError ? x.message : String(x));
        } finally {
          setBusy(false);
        }
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button type="submit" variant="primary" loading={busy}>
            {t('chown.apply')}
          </Button>
        </>
      }
    >
      <div style={{ display: 'grid', gap: 12 }}>
        <Input label={t('col.owner')} value={owner} onChange={(e) => setOwner(e.target.value)} mono autoComplete="off" />
        <Input label={t('chown.group')} value={group} onChange={(e) => setGroup(e.target.value)} mono autoComplete="off" />
        {entry.type === 'dir' && <Checkbox checked={rec} onChange={setRec} label={t('chown.recursive')} />}
        {err && (
          <div className="ui-form-err" role="alert">
            <Icon name="alert" />
            {err}
          </div>
        )}
      </div>
    </Dialog>
  );
}

/* ---------- simple text editor ---------- */
export function EditorDialog({ entry, admin, onClose }: { entry: FEntry; admin: boolean; onClose(): void }) {
  const t = useT('files');
  const [orig, setOrig] = useState<string | null>(null);
  const [text, setText] = useState('');
  const [mtime, setMtime] = useState(0);
  const [enc, setEnc] = useState('utf-8');
  const [err, setErr] = useState('');
  const [conflict, setConflict] = useState(false);
  const [busy, setBusy] = useState(false);
  const [readOnly, setReadOnly] = useState(false);
  const [useAdmin, setUseAdmin] = useState(admin);

  const load = async () => {
    setErr('');
    setConflict(false);
    try {
      const r = await fcall<{ content: string; mtime: number; encoding: string; truncated: boolean }>('files.readText', { path: entry.path, maxBytes: 2 << 20 }, { admin: useAdmin, onElevate: () => setUseAdmin(true) });
      setOrig(r.content);
      setText(r.content);
      setMtime(r.mtime);
      setEnc(r.encoding);
      setReadOnly(r.truncated);
    } catch (x) {
      setErr(x instanceof ApiError ? (x.code === 'invalid' ? t('edit.binary') : x.message) : String(x));
      setOrig('');
      setReadOnly(true);
    }
  };
  useEffect(() => {
    void load();
  }, [entry.path]); // eslint-disable-line react-hooks/exhaustive-deps

  const dirty = orig !== null && text !== orig;
  const save = async (force = false) => {
    setBusy(true);
    setErr('');
    try {
      const r = await fcall<{ mtime: number }>('files.writeText', { path: entry.path, content: text, encoding: enc, expectedMtime: force ? 0 : mtime }, { admin: useAdmin, onElevate: () => setUseAdmin(true) });
      setOrig(text);
      setMtime(r.mtime);
      setConflict(false);
      toast.ok(t('edit.saved', { name: entry.name }));
      emitChanged(dirname(entry.path));
    } catch (x) {
      if (x instanceof ApiError && x.code === 'conflict') setConflict(true);
      else setErr(x instanceof ApiError ? x.message : String(x));
    } finally {
      setBusy(false);
    }
  };
  const [discard, setDiscard] = useState(false);
  const close = () => {
    if (dirty) setDiscard(true);
    else onClose();
  };
  return (
    <Dialog
      open
      size="lg"
      onClose={close}
      title={entry.name}
      description={<span className="mono">{entry.path}</span>}
      icon="edit"
      onSubmit={(e) => {
        e.preventDefault();
        if (!readOnly && dirty) void save();
      }}
      footer={
        <>
          <Badge tone={dirty ? 'warn' : 'neutral'}>{dirty ? t('edit.unsaved') : enc}</Badge>
          <span style={{ flex: 1 }} />
          {discard ? (
            <>
              <Button variant="ghost" onClick={() => setDiscard(false)}>
                {t('edit.keep')}
              </Button>
              <Button variant="danger-solid" onClick={onClose}>
                {t('edit.discardBtn')}
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" onClick={close}>
                {t('edit.close')}
              </Button>
              <Button type="submit" variant="primary" loading={busy} disabled={readOnly || !dirty}>
                {t('save')}
              </Button>
            </>
          )}
        </>
      }
    >
      {orig === null ? (
        <p className="files-muted">{t('loading')}</p>
      ) : (
        <>
          <Textarea
            mono
            value={text}
            readOnly={readOnly}
            spellCheck={false}
            rows={18}
            aria-label={t('edit.area')}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
                e.preventDefault();
                if (!readOnly && dirty) void save();
              }
              if (e.key === 'Tab' && !e.shiftKey && !readOnly) {
                e.preventDefault();
                const el = e.currentTarget;
                const s = el.selectionStart;
                setText(text.slice(0, s) + '  ' + text.slice(el.selectionEnd));
                requestAnimationFrame(() => el.setSelectionRange(s + 2, s + 2));
              }
            }}
          />
          {readOnly && !err && <p className="files-muted">{t('edit.readOnly')}</p>}
          {discard && <p className="files-muted">{t('edit.discard')}</p>}
          {conflict && (
            <div className="ui-form-err" role="alert">
              <Icon name="alert" />
              <span>{t('edit.conflict')}</span>
              <Button size="sm" onClick={() => void load()}>
                {t('edit.reload')}
              </Button>
              <Button size="sm" variant="danger" onClick={() => void save(true)}>
                {t('edit.overwrite')}
              </Button>
            </div>
          )}
          {err && (
            <div className="ui-form-err" role="alert">
              <Icon name="alert" />
              {err}
            </div>
          )}
        </>
      )}
    </Dialog>
  );
}
