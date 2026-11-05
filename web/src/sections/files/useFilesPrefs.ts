import { useMemo } from 'react';
import { usePrefs } from '../../api';
import type { TFn } from '../../i18n';
import { toast } from '../../ui';
import { fail } from './filesOps';
import type { FEntry, Remote } from './types';
import { basename } from './util';

type Prefs = ReturnType<typeof usePrefs>['prefs'];

function readFilePrefs(prefs: Prefs) {
  const starredList: string[] = Array.isArray(prefs['files.starred']) ? prefs['files.starred'] : [];
  const bookmarks: string[] = Array.isArray(prefs['files.bookmarks'])
    ? prefs['files.bookmarks'].filter((x: unknown) => typeof x === 'string')
    : [];
  const remotes: Remote[] = Array.isArray(prefs['files.remotes'])
    ? prefs['files.remotes'].filter((r: any) => r && typeof r.name === 'string')
    : [];
  const showHidden = !!prefs['files.showHidden'];
  return { starredList, bookmarks, remotes, showHidden };
}

/** File-manager prefs (stars, bookmarks, remotes, hidden files) and the actions that change them. */
export function useFilesPrefs(t: TFn) {
  const { prefs, ready, set } = usePrefs();
  const { starredList, bookmarks, remotes, showHidden } = readFilePrefs(prefs);
  const starred = useMemo(() => new Set(starredList), [starredList.join('\n')]); // eslint-disable-line react-hooks/exhaustive-deps

  const toggleStar = async (e: FEntry) => {
    const next = starred.has(e.path)
      ? starredList.filter((p) => p !== e.path)
      : [...starredList, e.path];
    await set('files.starred', next).catch((x) => fail(t('err.prefs'), x));
    toast.ok(
      starred.has(e.path) ? t('unstarred', { name: e.name }) : t('starredDone', { name: e.name }),
    );
  };
  const addBookmark = async (path: string) => {
    if (bookmarks.includes(path)) return toast.info(t('bookmarkExists'));
    await set('files.bookmarks', [...bookmarks, path]).catch((x) => fail(t('err.prefs'), x));
    toast.ok(t('bookmarked', { name: basename(path) || '/' }));
  };
  const removeBookmark = (path: string) =>
    void set(
      'files.bookmarks',
      bookmarks.filter((b) => b !== path),
    ).catch((x) => fail(t('err.prefs'), x));
  const toggleHidden = () => void set('files.showHidden', !showHidden);

  return {
    ready,
    set,
    savedTabs: prefs['files.tabs'] as unknown,
    starredList,
    starred,
    bookmarks,
    remotes,
    showHidden,
    toggleStar,
    addBookmark,
    removeBookmark,
    toggleHidden,
  };
}

export type FilesPrefs = ReturnType<typeof useFilesPrefs>;
