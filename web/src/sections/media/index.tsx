import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { openApp } from '../../lib/openApp';
import { apiUrl, errorMessage, useSession } from '../../api';
import { useT } from '../../i18n';
import { formatBytes, formatTime } from '../../lib/format';
import { useWindowActivity } from '../../lib/windowActivity';
import { Button, Dialog, Icon, IconButton, Skeleton, registerIcon } from '../../ui';
import { FilePicker } from '../files/FilePicker';
import { fcall, fileDownloadUrl } from '../files/fapi';
import { remoteTarget } from '../files/remoteLocation';
import { Thumb } from '../files/Thumb';
import type { FEntry, RawEntry } from '../files/types';
import { withPath } from '../files/util';
import { isMediaEntry, mediaKind, validMediaPath } from './model';
import './media.css';

registerIcon(
  'media-fit',
  '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M7 10V8h3M17 14v2h-3"/>',
);
registerIcon('media-rotate', '<path d="M4 9a8 8 0 1 1 0 7M4 4v5h5"/>');
registerIcon('media-fullscreen', '<path d="M8 3H3v5M16 3h5v5M21 16v5h-5M3 16v5h5"/>');
registerIcon('media-pause', '<path d="M8 5v14M16 5v14"/>');

function parentPath(value: string) {
  const remote = remoteTarget(value);
  if (remote) {
    const i = remote.path.lastIndexOf('/');
    return `remote:${remote.conn}:${i <= 0 ? '/' : remote.path.slice(0, i)}`;
  }
  const normalized = value.replaceAll('\\', '/'),
    i = normalized.lastIndexOf('/');
  return i <= 0
    ? '/'
    : i === 2 && normalized[1] === ':'
      ? normalized.slice(0, 3)
      : normalized.slice(0, i);
}
function mediaURL(path: string, admin: boolean) {
  const remote = remoteTarget(path);
  const params = new URLSearchParams({ path: remote?.path ?? path });
  if (remote) params.set('remote', remote.conn);
  else if (admin) params.set('admin', '1');
  return apiUrl(`/api/files/media?${params}`);
}

export default function MediaPage() {
  const t = useT('media');
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const path = params.get('path') ?? '';
  const requestedAdmin = params.get('admin') === '1' && !remoteTarget(path);
  const active = useWindowActivity();
  const { session } = useSession();
  const [entry, setEntry] = useState<FEntry | null>(null);
  const [items, setItems] = useState<FEntry[]>([]);
  const [admin, setAdmin] = useState(requestedAdmin);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [decodeError, setDecodeError] = useState(false);
  const [open, setOpen] = useState(false);
  const [details, setDetails] = useState(false),
    [strip, setStrip] = useState(true);
  const [natural, setNatural] = useState({ width: 0, height: 0 });
  const [dimensions, setDimensions] = useState({ width: 1, height: 1 });
  const [rotation, setRotation] = useState(0),
    [zoom, setZoom] = useState<number | null>(null);
  const [pan, setPan] = useState({ x: 0, y: 0 }),
    [slideshow, setSlideshow] = useState(false);
  const [duration, setDuration] = useState(0);
  const frame = useRef<HTMLDivElement>(null),
    stage = useRef<HTMLDivElement>(null);
  const playback = useRef<HTMLVideoElement | null>(null);
  const drag = useRef<{ id: number; x: number; y: number; pan: { x: number; y: number } } | null>(
    null,
  );
  const kind = entry ? mediaKind(entry) : null;
  const current = items.findIndex((item) => item.path === path);
  const turn = rotation % 180 !== 0;
  const fitScale = Math.min(
    1,
    (dimensions.width - 32) / (turn ? natural.height : natural.width || 1),
    (dimensions.height - 32) / (turn ? natural.width : natural.height || 1),
  );
  const scale = zoom === null ? Math.max(0.01, fitScale) : zoom / 100;
  const visibleItems = items.slice(Math.max(0, current - 15), Math.max(30, current + 16));

  useLayoutEffect(() => {
    const element = stage.current;
    if (!element) return;
    const observer = new ResizeObserver((entries) => {
      const box = entries[0].contentRect;
      setDimensions({ width: box.width, height: box.height });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [entry?.path]);
  useEffect(() => {
    let disposed = false;
    const controller = new AbortController();
    setEntry(null);
    setItems([]);
    setError('');
    setDecodeError(false);
    setNatural({ width: 0, height: 0 });
    setZoom(null);
    setRotation(0);
    setPan({ x: 0, y: 0 });
    setDuration(0);
    setAdmin(requestedAdmin);
    setLoading(false);
    if (!path) return;
    if (!validMediaPath(path)) {
      setError(t('invalidPath'));
      return;
    }
    setLoading(true);
    const options = {
      admin: requestedAdmin,
      signal: controller.signal,
      onElevate: () => {
        if (!disposed) setAdmin(true);
      },
    };
    void (async () => {
      try {
        const value = await fcall<FEntry>('files.stat', { path }, options);
        if (disposed) return;
        if (!isMediaEntry(value) || (value.type !== 'file' && value.targetType !== 'file'))
          throw new Error(t('notMedia'));
        const selected = { ...value, path };
        setEntry(selected);
        try {
          const listing = await fcall<{ entries: RawEntry[] }>(
            'files.list',
            { path: parentPath(path) },
            options,
          );
          if (disposed) return;
          const gallery = listing.entries
            .filter(
              (item) => isMediaEntry(item) && (item.type === 'file' || item.targetType === 'file'),
            )
            .slice(0, 2000)
            .map((item) => withPath(parentPath(path), item))
            .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));
          if (!gallery.some((item) => item.path === path)) gallery.unshift(selected);
          setItems(gallery);
        } catch {
          if (!disposed) setItems([selected]);
        }
      } catch (problem) {
        if (!disposed) setError(errorMessage(problem));
      } finally {
        if (!disposed) setLoading(false);
      }
    })();
    return () => {
      disposed = true;
      controller.abort();
    };
  }, [path, requestedAdmin, t]);
  const choose = useCallback(
    (next: string) => {
      setSlideshow(false);
      setParams({ path: next, ...(admin && !remoteTarget(next) ? { admin: '1' } : {}) });
    },
    [admin, setParams],
  );
  const step = useCallback(
    (offset: number) => {
      if (items.length > 1) {
        const index = (Math.max(0, current) + offset + items.length) % items.length;
        choose(items[index].path);
      }
    },
    [choose, current, items],
  );
  useEffect(() => {
    if (!active) playback.current?.pause();
  }, [active]);
  useEffect(() => {
    if (!slideshow || !active || kind !== 'image' || items.length < 2) return;
    const timer = setTimeout(() => {
      const next =
        items.find((item, index) => index > current && mediaKind(item) === 'image') ??
        items.find((item) => mediaKind(item) === 'image');
      if (next) {
        setParams({
          path: next.path,
          ...(admin && !remoteTarget(next.path) ? { admin: '1' } : {}),
        });
      }
    }, 5000);
    return () => clearTimeout(timer);
  }, [slideshow, active, kind, items, current, setParams, admin]);
  const zoomBy = (factor: number) => {
    setZoom(Math.round(Math.max(5, Math.min(800, scale * 100 * factor))));
  };
  const fit = () => {
    setZoom(null);
    setPan({ x: 0, y: 0 });
  };
  const launch = (to: string) => {
    openApp(to, navigate);
  };
  const fullscreen = () => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => {});
    else void frame.current?.requestFullscreen().catch(() => {});
  };
  const leaveFullscreen = async () => {
    if (document.fullscreenElement) await document.exitFullscreen().catch(() => {});
  };
  const openFile = async () => {
    await leaveFullscreen();
    setOpen(true);
  };
  const fileInfo = async () => {
    await leaveFullscreen();
    setDetails(true);
  };
  const status = loading
    ? t('loading')
    : entry
      ? `${current + 1} / ${items.length || 1} · ${formatBytes(entry.size)}`
      : t('title');

  return (
    <div
      className="media-viewer"
      ref={frame}
      tabIndex={-1}
      onKeyDown={(event) => {
        const target = event.target as HTMLElement;
        if (
          event.ctrlKey ||
          event.metaKey ||
          event.altKey ||
          target.closest('input,textarea,select,video,audio')
        )
          return;
        if (event.key === 'ArrowLeft') {
          event.preventDefault();
          step(-1);
        } else if (event.key === 'ArrowRight') {
          event.preventDefault();
          step(1);
        } else if (kind === 'image' && ['+', '=', '-', '0', 'r', 'R'].includes(event.key)) {
          event.preventDefault();
          if (event.key === '0') fit();
          else if (event.key.toLowerCase() === 'r') {
            setRotation((value) => (value + 90) % 360);
            fit();
          } else zoomBy(event.key === '-' ? 0.8 : 1.25);
        } else if (event.key.toLowerCase() === 'f') {
          event.preventDefault();
          fullscreen();
        }
      }}
    >
      <header className="media-toolbar">
        <div className="media-file-heading">
          <span className="media-brand-icon">
            <Icon name="image" size={21} />
          </span>
          <div>
            <strong>{entry?.name ?? t('title')}</strong>
            <small>{entry ? parentPath(path) : t('subtitle')}</small>
          </div>
        </div>
        <Button icon="files" onClick={() => void openFile()}>
          {t('open')}
        </Button>
        {entry && (
          <>
            <IconButton icon="info" label={t('info')} onClick={() => void fileInfo()} />
            <a
              className="ui-btn media-download"
              href={fileDownloadUrl(path, admin)}
              download={entry.name}
            >
              <Icon name="download" />
              <span>{t('download')}</span>
            </a>
          </>
        )}
        <IconButton icon="media-fullscreen" label={t('fullscreen')} onClick={fullscreen} />
      </header>
      {entry ? (
        <>
          <div className="media-actions" role="toolbar" aria-label={t('controls')}>
            <IconButton
              icon="chevronleft"
              label={t('previous')}
              disabled={items.length < 2}
              onClick={() => step(-1)}
            />
            <IconButton
              icon="right"
              label={t('next')}
              disabled={items.length < 2}
              onClick={() => step(1)}
            />
            {kind === 'image' && (
              <>
                <span className="media-tool-group">
                  <IconButton icon="minus" label={t('zoomOut')} onClick={() => zoomBy(0.8)} />
                  <button className="media-zoom-value" onClick={fit} title={t('fit')}>
                    {Math.round(scale * 100)}%
                  </button>
                  <IconButton icon="plus" label={t('zoomIn')} onClick={() => zoomBy(1.25)} />
                </span>
                <IconButton icon="media-fit" label={t('fit')} onClick={fit} />
                <IconButton
                  icon="media-rotate"
                  label={t('rotate')}
                  onClick={() => {
                    setRotation((value) => (value + 90) % 360);
                    fit();
                  }}
                />
                <IconButton
                  icon={slideshow ? 'media-pause' : 'play'}
                  label={slideshow ? t('stopSlideshow') : t('slideshow')}
                  disabled={items.filter((item) => mediaKind(item) === 'image').length < 2}
                  onClick={() => setSlideshow((value) => !value)}
                />
              </>
            )}
            <span className="media-actions-spacer" />
            <IconButton
              icon="list"
              label={t('filmstrip')}
              aria-pressed={strip}
              onClick={() => setStrip((value) => !value)}
            />
            <Button
              variant="ghost"
              size="sm"
              icon="files"
              onClick={() =>
                launch(`/files?path=${encodeURIComponent(path)}${admin ? '&admin=1' : ''}`)
              }
            >
              {t('explorer')}
            </Button>
          </div>
          <div
            ref={stage}
            className={`media-stage${kind === 'image' && zoom !== null ? ' is-zoomed' : ''}`}
            tabIndex={0}
            aria-label={t('canvas')}
            onPointerDown={(event) => {
              if (kind !== 'image' || zoom === null || event.button !== 0) return;
              event.currentTarget.setPointerCapture(event.pointerId);
              drag.current = { id: event.pointerId, x: event.clientX, y: event.clientY, pan };
            }}
            onPointerMove={(event) => {
              const start = drag.current;
              if (start && start.id === event.pointerId)
                setPan({
                  x: start.pan.x + event.clientX - start.x,
                  y: start.pan.y + event.clientY - start.y,
                });
            }}
            onPointerUp={(event) => {
              if (drag.current?.id === event.pointerId) {
                drag.current = null;
                if (event.currentTarget.hasPointerCapture(event.pointerId))
                  event.currentTarget.releasePointerCapture(event.pointerId);
              }
            }}
            onPointerCancel={() => {
              drag.current = null;
            }}
          >
            {!decodeError &&
              (kind === 'image' ? (
                <img
                  key={path}
                  className="media-photo"
                  src={mediaURL(path, admin)}
                  alt={entry.name}
                  draggable={false}
                  style={{
                    width: natural.width ? natural.width * scale : undefined,
                    height: natural.height ? natural.height * scale : undefined,
                    transform: `translate(${pan.x}px,${pan.y}px) rotate(${rotation}deg)`,
                  }}
                  onLoad={(event) =>
                    setNatural({
                      width: event.currentTarget.naturalWidth,
                      height: event.currentTarget.naturalHeight,
                    })
                  }
                  onError={() => setDecodeError(true)}
                />
              ) : (
                <div className={`media-player${kind === 'audio' ? ' is-audio' : ''}`}>
                  {kind === 'audio' && (
                    <div className="media-audio-cover">
                      <Icon name="music" size={76} />
                      <strong>{entry.name}</strong>
                    </div>
                  )}
                  <video
                    key={path}
                    ref={playback}
                    controls
                    playsInline
                    preload="metadata"
                    src={mediaURL(path, admin)}
                    aria-label={entry.name}
                    onLoadedMetadata={(event) => {
                      setNatural({
                        width: event.currentTarget.videoWidth,
                        height: event.currentTarget.videoHeight,
                      });
                      setDuration(event.currentTarget.duration);
                    }}
                    onError={() => setDecodeError(true)}
                  />
                </div>
              ))}
            {decodeError && (
              <div className="media-empty">
                <Icon name="video" size={46} />
                <h2>{t('unsupportedTitle')}</h2>
                <p>{t('unsupported')}</p>
                <Button icon="refresh" onClick={() => setDecodeError(false)}>
                  {t('retry')}
                </Button>
                <a className="ui-btn" href={fileDownloadUrl(path, admin)} download={entry.name}>
                  {t('download')}
                </a>
              </div>
            )}
          </div>
          {strip && items.length > 1 && (
            <nav className="media-filmstrip" aria-label={t('gallery')}>
              {visibleItems.map((item) => (
                <button
                  key={item.path}
                  className={item.path === path ? 'is-current' : ''}
                  aria-label={item.name}
                  aria-current={item.path === path ? 'true' : undefined}
                  onClick={() => choose(item.path)}
                >
                  <Thumb e={item} admin={admin} />
                  <span>{item.name}</span>
                </button>
              ))}
            </nav>
          )}
          <footer className="media-status">
            <span>{status}</span>
            <span>
              {natural.width > 0
                ? `${natural.width} × ${natural.height}`
                : kind === 'audio'
                  ? t('audio')
                  : ''}
            </span>
            <span>{t('shortcutHint')}</span>
          </footer>
        </>
      ) : (
        <div className="media-empty">
          {loading ? (
            <Skeleton lines={4} />
          ) : (
            <>
              <span className="media-empty-icon">
                <Icon name="image" size={52} />
              </span>
              <h1>{t('title')}</h1>
              <p>{error || t('empty')}</p>
              <Button icon="files" onClick={() => void openFile()}>
                {t('open')}
              </Button>
            </>
          )}
        </div>
      )}
      {open && <FilePicker mode="file" title={t('openTitle')} initialDirectory={path?parentPath(path):session?.home||'/'} admin={admin} accept={isMediaEntry} onCancel={()=>setOpen(false)} onChoose={selected=>{if(validMediaPath(selected)){choose(selected);setOpen(false);}}} />}
      <Dialog
        open={details && !!entry}
        title={t('info')}
        icon="info"
        onClose={() => setDetails(false)}
        footer={<Button onClick={() => setDetails(false)}>{t('close')}</Button>}
      >
        <dl className="media-properties">
          <dt>{t('name')}</dt>
          <dd>{entry?.name}</dd>
          <dt>{t('path')}</dt>
          <dd>{path}</dd>
          <dt>{t('size')}</dt>
          <dd>{formatBytes(entry?.size ?? 0)}</dd>
          <dt>{t('type')}</dt>
          <dd>{entry?.mime || kind}</dd>
          {natural.width > 0 && (
            <>
              <dt>{t('dimensions')}</dt>
              <dd>
                {natural.width} × {natural.height}
              </dd>
            </>
          )}
          {Number.isFinite(duration) && duration > 0 && (
            <>
              <dt>{t('duration')}</dt>
              <dd>
                {Math.floor(duration / 60)}:{String(Math.floor(duration % 60)).padStart(2, '0')}
              </dd>
            </>
          )}
          {!!entry?.mtime && (
            <>
              <dt>{t('modified')}</dt>
              <dd>{formatTime(entry.mtime)}</dd>
            </>
          )}
        </dl>
      </Dialog>
    </div>
  );
}
