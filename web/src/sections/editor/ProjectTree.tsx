import { useEffect, useRef, useState } from 'react';
import { errorMessage } from '../../api';
import { useT } from '../../i18n';
import { Button, Icon, IconButton } from '../../ui';
import { fcall } from '../files/fapi';
import { join } from '../files/util';
import type { RawEntry } from '../files/types';
import { documentName } from './document';

export interface ProjectRoot {
  path: string;
  admin: boolean;
}
interface Listing {
  entries: RawEntry[];
  loading: boolean;
  error: string;
  truncated: boolean;
}

/** Only expanded folders are requested; no recursive scans or background watchers. */
export function ProjectTree({
  root,
  onOpen,
  onElevate,
}: {
  root: ProjectRoot;
  onOpen(path: string): void;
  onElevate(): void;
}) {
  const t = useT('editor');
  const [nodes, setNodes] = useState<Record<string, Listing>>({});
  const [expanded, setExpanded] = useState(new Set([root.path]));
  const controllers = useRef(new Map<string, AbortController>());
  const latest = useRef({ root, onElevate });
  latest.current = { root, onElevate };
  const live = useRef(true);
  const load = async (path: string) => {
    controllers.current.get(path)?.abort();
    const controller = new AbortController();
    controllers.current.set(path, controller);
    setNodes((nodes) => ({
      ...nodes,
      [path]: { entries: nodes[path]?.entries ?? [], loading: true, error: '', truncated: false },
    }));
    try {
      const result = await fcall<{ entries: RawEntry[]; truncated?: boolean }>(
        'files.list',
        { path, showHidden: true },
        {
          admin: latest.current.root.admin,
          signal: controller.signal,
          onElevate: () => latest.current.onElevate(),
        },
      );
      if (!live.current || controller.signal.aborted) return;
      const entries = [...result.entries]
        .filter(
          (entry) =>
            (entry.type === 'file' || entry.type === 'dir') &&
            entry.name !== '.' &&
            entry.name !== '..' &&
            !/[\\/\0]/.test(entry.name),
        )
        .sort(
          (a, b) =>
            Number(b.type === 'dir') - Number(a.type === 'dir') ||
            a.name.localeCompare(b.name, undefined, { numeric: true }),
        );
      setNodes((nodes) => {
        const otherCount = Object.entries(nodes).reduce(
          (count, [key, listing]) => count + (key === path ? 0 : listing.entries.length),
          0,
        );
        const limit = Math.max(0, Math.min(1000, 5000 - otherCount));
        return {
          ...nodes,
          [path]: {
            entries: entries.slice(0, limit),
            loading: false,
            error: '',
            truncated: !!result.truncated || entries.length > limit,
          },
        };
      });
    } catch (error) {
      if (live.current && !controller.signal.aborted)
        setNodes((nodes) => ({
          ...nodes,
          [path]: { entries: [], loading: false, error: errorMessage(error), truncated: false },
        }));
    } finally {
      if (controllers.current.get(path) === controller) controllers.current.delete(path);
    }
  };
  useEffect(() => {
    live.current = true;
    const pending = controllers.current;
    void load(root.path);
    return () => {
      live.current = false;
      for (const controller of pending.values()) controller.abort();
      pending.clear();
    };
    // The component is keyed by project path; privilege changes affect subsequent requests.
  }, [root.path]);
  const toggle = (path: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
    if (!expanded.has(path) && !nodes[path]) void load(path);
  };
  const children = (path: string, level: number) => {
    if (level > 40) return <p className="editor-tree-notice">{t('project.treeLimit')}</p>;
    const listing = nodes[path];
    return (
      <div role="group">
        {listing?.loading && (
          <p className="editor-tree-notice" role="status">
            {t('project.loading')}
          </p>
        )}
        {listing?.error && (
          <div className="editor-tree-notice" role="alert">
            <span>{listing.error}</span>
            <Button size="sm" onClick={() => void load(path)}>
              {t('project.retry')}
            </Button>
          </div>
        )}
        {listing?.entries.map((entry) => {
          const name = join(path, entry.name),
            directory = entry.type === 'dir',
            open = expanded.has(name);
          return (
            <div
              role="treeitem"
              aria-level={level}
              aria-expanded={directory ? open : undefined}
              key={name}
            >
              <button
                type="button"
                className="editor-tree-item"
                data-project-path={name}
                style={{ paddingInlineStart: 9 + (level - 1) * 14 }}
                onClick={() => (directory ? toggle(name) : onOpen(name))}
                onKeyDown={(event) => {
                  if (
                    (directory && event.key === 'ArrowRight' && !open) ||
                    (directory && event.key === 'ArrowLeft' && open)
                  ) {
                    event.preventDefault();
                    toggle(name);
                  }
                }}
                title={entry.name}
              >
                {directory ? (
                  <Icon
                    name="chevron"
                    size={13}
                    className={`editor-tree-chevron${open ? ' is-expanded' : ''}`}
                  />
                ) : (
                  <span className="editor-tree-indent" />
                )}
                <Icon name={directory ? 'files' : 'file'} size={15} />
                <span>{entry.name}</span>
              </button>
              {directory && open && children(name, level + 1)}
            </div>
          );
        })}
        {listing?.truncated && <p className="editor-tree-notice">{t('project.treeLimit')}</p>}
        {listing && !listing.loading && !listing.error && !listing.entries.length && (
          <p className="editor-tree-notice">{t('project.empty')}</p>
        )}
      </div>
    );
  };
  return (
    <div className="editor-project-tree" role="tree" aria-label={t('project.files')}>
      <div className="editor-tree-root">
        <Icon name="files" size={15} />
        <b title={root.path}>{documentName(root.path)}</b>
        <IconButton
          icon="refresh"
          label={t('project.refresh')}
          size="sm"
          variant="ghost"
          onClick={() => {
            setNodes({});
            setExpanded(new Set([root.path]));
            void load(root.path);
          }}
        />
      </div>
      {children(root.path, 1)}
    </div>
  );
}
