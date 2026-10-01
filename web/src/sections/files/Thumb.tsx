import { useEffect, useRef, useState } from 'react';
import { downloadUrl } from '../../api';
import { Icon } from '../../ui';
import { cachedSnippet, cachedThumb, loadSnippet, loadThumb, thumbKey } from './fapi';
import { KIND_ICON, canInlineImage, canThumb, kindOf, looksTextual } from './kinds';
import type { FEntry } from './types';

function useVisible<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [vis, setVis] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el || vis) return;
    if (typeof IntersectionObserver === 'undefined') {
      setVis(true);
      return;
    }
    const io = new IntersectionObserver((es) => es.some((x) => x.isIntersecting) && setVis(true), { rootMargin: '120px' });
    io.observe(el);
    return () => io.disconnect();
  }, [vis]);
  return [ref, vis] as const;
}

/** The preview area of a file card: image thumbnail, first lines of a text file, or a type tile. */
export function Thumb({ e, admin }: { e: FEntry; admin: boolean }) {
  const kind = kindOf(e);
  const [ref, vis] = useVisible<HTMLDivElement>();
  const stamp = `${e.mtime}:${e.size}`;
  const isThumb = kind === 'img' && canThumb(e.name) && e.size > 0;
  const isText = (kind === 'code' || kind === 'txt' || kind === 'other') && looksTextual(e) && e.size > 0 && e.size < 4 << 20 && !!e.path;
  const [src, setSrc] = useState<string | null | undefined>(() => (isThumb ? cachedThumb(thumbKey(admin, e.path, stamp)) : undefined));
  const [text, setText] = useState<string | null | undefined>(() => (isText ? cachedSnippet(thumbKey(admin, e.path, stamp)) : undefined));

  useEffect(() => {
    if (!vis || !e.path) return;
    let live = true;
    if (isThumb && src === undefined) void loadThumb(e.path, stamp, admin).then((v) => live && setSrc(v));
    if (isText && text === undefined) void loadSnippet(e.path, stamp, admin).then((v) => live && setText(v));
    return () => {
      live = false;
    };
  }, [vis, e.path, stamp, admin, isThumb, isText, src, text]);

  let inner;
  if (kind === 'img' && src) inner = <img className="files-th-img" src={src} alt="" draggable={false} />;
  else if (kind === 'img' && !isThumb && canInlineImage(e.name) && e.size < 6 << 20 && vis && e.path) inner = <img className="files-th-img" src={downloadUrl(e.path, admin, true)} alt="" loading="lazy" draggable={false} />;
  else if (isText && text) inner = <pre className="files-th-pre">{text}</pre>;
  else
    inner = (
      <span className="files-ic files-ic--big">
        <Icon name={KIND_ICON[kind]} />
      </span>
    );
  const cls = kind === 'img' && (src || (!isThumb && canInlineImage(e.name))) ? 'files-th files-th--img' : isText && text ? 'files-th files-th--txt' : 'files-th files-th--big';
  return (
    <div ref={ref} className={cls}>
      {inner}
    </div>
  );
}
