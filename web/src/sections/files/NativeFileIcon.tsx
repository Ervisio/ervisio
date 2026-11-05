import { Icon } from '../../ui';
import { KIND_ICON, kindOf } from './kinds';
import type { FEntry } from './types';

/** The same folder/document artwork in icon view, details view and Properties. */
export function NativeFileIcon({ entry, compact = false }: { entry: FEntry; compact?: boolean }) {
  const kind = kindOf(entry);
  const dot = entry.name.lastIndexOf('.');
  const extension =
    kind === 'dir' || dot < 0
      ? ''
      : entry.name
          .slice(dot + 1)
          .slice(0, 5)
          .toUpperCase();
  return (
    <span
      className={`files-os-icon files-os-icon--${kind}${compact ? ' is-compact' : ''}`}
      data-file-kind={kind}
      data-file-ext={extension || undefined}
      aria-hidden="true"
    >
      {kind === 'dir' ? (
        <span className="files-os-folder" />
      ) : (
        <span className="files-os-paper">
          <Icon name={KIND_ICON[kind]} />
          {extension && <i>{extension}</i>}
        </span>
      )}
      {entry.type === 'symlink' && (
        <span className="files-os-link">
          <Icon name="link" />
        </span>
      )}
    </span>
  );
}
