import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { ApiError, errorMessage, usePrefs, useSession } from '../../api';
import { useT } from '../../i18n';
import type { WindowCloseRequest } from '../../lib/windowClose';
import { Button, Dialog, Icon, IconButton, Input, Skeleton, toast } from '../../ui';
import { emitChanged, fcall } from '../files/fapi';
import { isRemoteLocation, remoteTarget } from '../files/remoteLocation';
import { dirname } from '../files/util';
import type { CodeControls } from './CodeSurface';
import {
  documentName,
  isDocumentDirty,
  newDocument,
  normalizeText,
  serializeText,
  textPosition,
  type EditorMode,
  type TextDocument,
} from './document';
import './editor.css';
import { useEditorDrafts } from './DraftsProvider';

const CodeSurface = lazy(() => import('./CodeSurface'));
const LanguagePicker = lazy(() => import('./LanguagePicker'));
const MAX_TEXT_BYTES = 2 << 20;
type PathDialog = { kind: 'open' | 'save'; value: string; error: string; busy: boolean };

export default function CodeEditor() {
  return <TextWorkspace mode="code" />;
}

export function TextWorkspace({ mode }: { mode: EditorMode }) {
  const t = useT('editor');
  const { session, host } = useSession();
  const { prefs, set } = usePrefs();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const { documents, setDocuments, activeId, setActiveId } = useEditorDrafts(mode);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [conflict, setConflict] = useState(false);
  const [dialog, setDialog] = useState<PathDialog | null>(null);
  const [replace, setReplace] = useState<{ path: string; mtime: number } | null>(null);
  const [pendingClose, setPendingClose] = useState<(() => void) | null>(null);
  const [find, setFind] = useState(false);
  const [search, setSearch] = useState('');
  const [position, setPosition] = useState(0);
  const [language, setLanguage] = useState('auto');
  const workspace = useRef<HTMLDivElement>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const code = useRef<CodeControls | null>(null);
  const latest = useRef(documents);
  latest.current = documents;
  const active = documents.find((d) => d.id === activeId) ?? documents[0];
  const settingsKey = `editor.${mode}`;
  const settings = (prefs[settingsKey] ?? {}) as { wrap?: boolean; fontSize?: number };
  const wrap = settings.wrap ?? mode === 'text';
  const fontSize = Math.min(28, Math.max(11, Number(settings.fontSize) || 14));
  const dirty = documents.some(isDocumentDirty);
  const location = textPosition(active.text, position);
  const name = active.path ? documentName(active.path) : t('untitled');
  const update = (id: string, patch: Partial<TextDocument>) =>
    setDocuments((list) => list.map((d) => (d.id === id ? { ...d, ...patch } : d)));
  const remember = (patch: Partial<typeof settings>) =>
    void set(settingsKey, { ...settings, ...patch });

  const openPath = useCallback(
    async (path: string, admin = false) => {
      const existing = latest.current.find((d) => d.path === path);
      if (existing) {
        setActiveId(existing.id);
        return;
      }
      setBusy(true);
      setError('');
      try {
        let elevated = admin && !isRemoteLocation(path);
        const file = await fcall<{
          content: string;
          encoding: string;
          mtime: number;
          truncated: boolean;
        }>(
          'files.readText',
          { path, maxBytes: MAX_TEXT_BYTES },
          {
            admin: elevated,
            onElevate: () => {
              elevated = true;
            },
          },
        );
        const text = normalizeText(file.content);
        const doc: TextDocument = {
          ...newDocument(),
          path,
          text,
          saved: text,
          encoding: file.encoding,
          mtime: file.mtime,
          eol: file.content.includes('\r\n') ? 'CRLF' : 'LF',
          admin: elevated,
          readOnly: file.truncated,
        };
        setDocuments((list) =>
          list.length === 1 && !list[0].path && !list[0].text ? [doc] : [...list, doc],
        );
        setActiveId(doc.id);
        setPosition(0);
        setConflict(false);
      } catch (e) {
        setError(errorMessage(e));
        throw e;
      } finally {
        setBusy(false);
      }
    },
    [setDocuments, setActiveId],
  );
  const requestedPath = params.get('path') ?? '';
  const requestedAdmin = params.get('admin') === '1';
  const lastLaunch = useRef('');
  useEffect(() => {
    const key = `${requestedAdmin}:${requestedPath}`;
    if (!requestedPath || lastLaunch.current === key) return;
    lastLaunch.current = key;
    void openPath(requestedPath, requestedAdmin).catch(() => {});
  }, [requestedPath, requestedAdmin, openPath]);

  useEffect(() => {
    if (!dirty) return;
    const close = (event: Event) => {
      const request = event as CustomEvent<WindowCloseRequest>;
      const id = workspace.current?.closest<HTMLElement>('[data-window-id]')?.dataset.windowId;
      if (id && request.detail.id === id) {
        event.preventDefault();
        setPendingClose(() => () => {
          setDocuments([newDocument()]);
          request.detail.close();
        });
      }
    };
    window.addEventListener('ervisio:before-window-close', close);
    return () => {
      window.removeEventListener('ervisio:before-window-close', close);
    };
  }, [dirty, setDocuments]);

  const newTab = () => {
    const doc = newDocument();
    setDocuments((list) => [...list, doc]);
    setActiveId(doc.id);
    setError('');
    setPosition(0);
  };
  const closeTab = (doc: TextDocument) => {
    const close = () =>
      setDocuments((list) => {
        const next = list.filter((d) => d.id !== doc.id);
        if (!next.length) next.push(newDocument());
        if (activeId === doc.id)
          setActiveId(next[Math.max(0, list.findIndex((d) => d.id === doc.id) - 1)].id);
        return next;
      });
    if (isDocumentDirty(doc)) setPendingClose(() => close);
    else close();
  };
  const showPathDialog = (kind: 'open' | 'save') => {
    if (busy || (kind === 'save' && active.readOnly)) return;
    const home = session?.home ?? (session?.os === 'windows' ? 'C:\\' : '/');
    setDialog({
      kind,
      value:
        active.path ||
        `${home.replace(/[\\/]$/, '')}/${kind === 'save' ? (mode === 'text' ? 'notes.txt' : 'untitled.txt') : ''}`,
      error: '',
      busy: false,
    });
  };
  const write = async (path: string, expectedMtime: number) => {
    const snapshot = latest.current.find((d) => d.id === active.id);
    if (!snapshot) return;
    if (snapshot.readOnly) {
      setError(t('readOnly'));
      return;
    }
    setBusy(true);
    setError('');
    setConflict(false);
    try {
      let elevated = snapshot.admin;
      const saved = await fcall<{ mtime: number }>(
        'files.writeText',
        {
          path,
          content: serializeText(snapshot.text, snapshot.eol),
          encoding: snapshot.encoding,
          expectedMtime,
        },
        {
          admin: elevated,
          onElevate: () => {
            elevated = true;
          },
        },
      );
      update(snapshot.id, { path, saved: snapshot.text, mtime: saved.mtime, admin: elevated });
      emitChanged(dirname(path));
      setDialog(null);
      setReplace(null);
      toast.ok(t('saved', { name: documentName(path) }));
    } catch (e) {
      if (e instanceof ApiError && e.code === 'conflict') setConflict(true);
      else setError(errorMessage(e));
      throw e;
    } finally {
      setBusy(false);
    }
  };
  const save = () => {
    if (active.readOnly || busy) return;
    if (!active.path) showPathDialog('save');
    else void write(active.path, active.mtime).catch(() => {});
  };
  const submitPath = async () => {
    if (!dialog) return;
    const path = dialog.value.trim();
    if (!/^(?:\/|[A-Za-z]:[\\/]|\\\\)/.test(path) && !isRemoteLocation(path)) {
      setDialog({ ...dialog, error: t('absolutePath') });
      return;
    }
    setDialog({ ...dialog, busy: true, error: '' });
    try {
      if (dialog.kind === 'open') {
        await openPath(path);
        setDialog(null);
        return;
      }
      if (path === active.path) {
        await write(path, active.mtime);
        return;
      }
      try {
        const existing = await fcall<{ mtime: number }>(
          'files.stat',
          { path },
          { admin: active.admin },
        );
        setReplace({ path, mtime: existing.mtime });
        setDialog((d) => (d ? { ...d, busy: false } : null));
        return;
      } catch (e) {
        if (!(e instanceof ApiError && e.code === 'not_found')) throw e;
      }
      // Reserve a new destination exclusively before writing, avoiding a stat/write overwrite race.
      const created = await fcall<{ mtime: number }>(
        'files.create',
        { path },
        { admin: active.admin },
      );
      await write(path, created.mtime);
    } catch (e) {
      setDialog((d) => (d ? { ...d, busy: false, error: errorMessage(e) } : null));
    }
  };
  const reload = async () => {
    if (isDocumentDirty(active)) {
      setPendingClose(() => () => void reloadNow());
      return;
    }
    await reloadNow();
  };
  const reloadNow = async () => {
    if (!active.path) return;
    setBusy(true);
    try {
      const file = await fcall<{
        content: string;
        mtime: number;
        encoding: string;
        truncated: boolean;
      }>(
        'files.readText',
        { path: active.path, maxBytes: MAX_TEXT_BYTES },
        { admin: active.admin },
      );
      const text = normalizeText(file.content);
      update(active.id, {
        text,
        saved: text,
        mtime: file.mtime,
        encoding: file.encoding,
        eol: file.content.includes('\r\n') ? 'CRLF' : 'LF',
        readOnly: file.truncated,
      });
      setConflict(false);
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const findNext = () => {
    const input = textarea.current;
    if (!input || !search) return;
    const text = active.text.toLocaleLowerCase();
    const index = text.indexOf(search.toLocaleLowerCase(), input.selectionEnd);
    const next = index >= 0 ? index : text.indexOf(search.toLocaleLowerCase());
    if (next >= 0) {
      input.focus();
      input.setSelectionRange(next, next + search.length);
      setPosition(next);
    }
  };

  return (
    <div
      ref={workspace}
      className={`editor-workspace editor-workspace--${mode}`}
      onKeyDown={(event) => {
        if (!(event.ctrlKey || event.metaKey)) return;
        if (event.key.toLowerCase() === 's') {
          event.preventDefault();
          if (event.shiftKey) showPathDialog('save');
          else save();
        }
        if (event.key.toLowerCase() === 'o') {
          event.preventDefault();
          showPathDialog('open');
        }
        if (event.key.toLowerCase() === 'f' && mode === 'text') {
          event.preventDefault();
          setFind(true);
        }
      }}
    >
      <div className="editor-tabs" role="tablist" aria-label={t('documents')}>
        {documents.map((doc) => (
          <div className={`editor-tab${doc.id === active.id ? ' is-active' : ''}`} key={doc.id}>
            <button
              role="tab"
              aria-selected={doc.id === active.id}
              onClick={() => {
                setActiveId(doc.id);
                setPosition(0);
                setError('');
                setConflict(false);
              }}
              title={doc.path || t('untitled')}
            >
              <Icon name={mode === 'code' ? 'code' : 'file'} size={15} />
              <span>{doc.path ? documentName(doc.path) : t('untitled')}</span>
              {isDocumentDirty(doc) && <i aria-label={t('unsaved')} />}
            </button>
            <button
              className="editor-tab-close"
              aria-label={t('closeDocument', {
                name: doc.path ? documentName(doc.path) : t('untitled'),
              })}
              onClick={() => closeTab(doc)}
            >
              <Icon name="close" size={13} />
            </button>
          </div>
        ))}
        <IconButton icon="plus" label={t('new')} onClick={newTab} size="sm" variant="ghost" />
      </div>
      <div className="editor-toolbar">
        <Button size="sm" variant="ghost" icon="files" onClick={() => showPathDialog('open')}>
          {t('open')}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          icon="save"
          disabled={active.readOnly || busy}
          onClick={save}
        >
          {t('save')}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => showPathDialog('save')}
          disabled={active.readOnly || busy}
        >
          {t('saveAs')}
        </Button>
        <IconButton
          icon="search"
          label={t('find')}
          variant="ghost"
          size="sm"
          onClick={() => (mode === 'code' ? code.current?.find() : setFind(!find))}
        />
        <span className="editor-toolbar-space" />
        <button
          className={`editor-wrap${wrap ? ' is-active' : ''}`}
          aria-pressed={wrap}
          onClick={() => remember({ wrap: !wrap })}
        >
          {t('wordWrap')}
        </button>
        <IconButton
          icon="minus"
          label={t('smaller')}
          variant="ghost"
          size="sm"
          disabled={fontSize <= 11}
          onClick={() => remember({ fontSize: fontSize - 1 })}
        />
        <span className="editor-font-size">{fontSize}</span>
        <IconButton
          icon="plus"
          label={t('larger')}
          variant="ghost"
          size="sm"
          disabled={fontSize >= 28}
          onClick={() => remember({ fontSize: fontSize + 1 })}
        />
      </div>
      {find && mode === 'text' && (
        <div className="editor-find">
          <Input
            compact
            aria-label={t('findText')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                findNext();
              }
            }}
            autoFocus
          />
          <Button size="sm" onClick={findNext}>
            {t('findNext')}
          </Button>
          <IconButton
            icon="close"
            label={t('closeSearch')}
            onClick={() => setFind(false)}
            variant="ghost"
            size="sm"
          />
        </div>
      )}
      {active.path && (
        <div className="editor-path" title={active.path}>
          <Icon name={isRemoteLocation(active.path) ? 'globe' : 'file'} size={13} />
          <span>{remoteTarget(active.path)?.path ?? active.path}</span>
          <small>{isRemoteLocation(active.path) ? t('networkFolder') : host?.hostname}</small>
        </div>
      )}
      {busy && (
        <div className="editor-progress" role="status">
          {t('working')}
        </div>
      )}
      {(error || conflict || active.readOnly) && (
        <div className="editor-message" role="alert">
          <Icon name="alert" />
          <span>{conflict ? t('conflict') : error || t('readOnly')}</span>
          {conflict && (
            <Button size="sm" onClick={() => void reload()}>
              {t('reload')}
            </Button>
          )}
        </div>
      )}
      <div className="editor-body">
        {mode === 'code' ? (
          <Suspense fallback={<Skeleton lines={8} />}>
            <CodeSurface
              key={active.id}
              value={active.text}
              name={name}
              language={language}
              label={t('codeArea')}
              readOnly={active.readOnly || busy}
              wrap={wrap}
              fontSize={fontSize}
              onChange={(text) => update(active.id, { text })}
              onPosition={setPosition}
              controls={(controls) => {
                code.current = controls;
              }}
            />
          </Suspense>
        ) : (
          <textarea
            key={active.id}
            ref={textarea}
            className="editor-notepad"
            aria-label={t('textArea')}
            value={active.text}
            readOnly={active.readOnly || busy}
            spellCheck={false}
            wrap={wrap ? 'soft' : 'off'}
            style={{ fontSize }}
            onChange={(e) => update(active.id, { text: e.target.value })}
            onSelect={(e) => setPosition(e.currentTarget.selectionStart)}
          />
        )}
      </div>
      <footer className="editor-status">
        <span>{t('position', location)}</span>
        <span>{t('characters', { count: active.text.length })}</span>
        <span className="editor-toolbar-space" />
        {mode === 'code' && (
          <Suspense fallback={<span>{t('automatic')}</span>}>
            <LanguagePicker
              value={language}
              onChange={setLanguage}
              label={t('language')}
              automatic={t('automatic')}
              plain={t('plainText')}
            />
          </Suspense>
        )}
        <span>{active.eol}</span>
        <span>{active.encoding.toUpperCase()}</span>
        <span>{active.admin ? t('administrator') : ''}</span>
        <button
          onClick={() => {
            const to = `${mode === 'text' ? '/editor' : '/notepad'}${active.path ? `?path=${encodeURIComponent(active.path)}${active.admin ? '&admin=1' : ''}` : ''}`;
            if (dirty) {
              toast.info(t('saveBeforeSwitch'));
              return;
            }
            window.dispatchEvent(
              new CustomEvent('ervisio:shell-action', { detail: { action: 'open-app', to } }),
            );
            navigate(to);
          }}
        >
          {t(mode === 'text' ? 'openCodeEditor' : 'openNotepad')}
        </button>
      </footer>
      {dialog && (
        <Dialog
          open
          onClose={() => !dialog.busy && setDialog(null)}
          title={t(dialog.kind === 'open' ? 'openTitle' : 'saveTitle')}
          icon="files"
          onSubmit={(e) => {
            e.preventDefault();
            void submitPath();
          }}
          footer={
            <>
              <Button variant="ghost" disabled={dialog.busy} onClick={() => setDialog(null)}>
                {t('cancel')}
              </Button>
              <Button type="submit" variant="primary" loading={dialog.busy}>
                {t(dialog.kind === 'open' ? 'open' : 'save')}
              </Button>
            </>
          }
        >
          <Input
            label={t('serverPath')}
            hint={t('serverPathHint')}
            value={dialog.value}
            onChange={(e) => setDialog({ ...dialog, value: e.target.value, error: '' })}
            error={dialog.error || undefined}
            autoFocus
            mono
            spellCheck={false}
          />
        </Dialog>
      )}
      {replace && (
        <Dialog
          open
          onClose={() => setReplace(null)}
          title={t('replaceTitle')}
          description={replace.path}
          icon="alert"
          footer={
            <>
              <Button variant="ghost" onClick={() => setReplace(null)}>
                {t('cancel')}
              </Button>
              <Button
                variant="danger-solid"
                loading={busy}
                onClick={() => void write(replace.path, replace.mtime).catch(() => {})}
              >
                {t('replace')}
              </Button>
            </>
          }
        >
          <p>{t('replaceWarning')}</p>
        </Dialog>
      )}
      {pendingClose && (
        <Dialog
          open
          onClose={() => setPendingClose(null)}
          title={t('unsavedTitle')}
          icon="edit"
          footer={
            <>
              <Button variant="primary" onClick={() => setPendingClose(null)}>
                {t('keepEditing')}
              </Button>
              <Button
                variant="danger"
                onClick={() => {
                  const close = pendingClose;
                  setPendingClose(null);
                  close();
                }}
              >
                {t('discard')}
              </Button>
            </>
          }
        >
          <p>{t('unsavedWarning')}</p>
        </Dialog>
      )}
    </div>
  );
}
