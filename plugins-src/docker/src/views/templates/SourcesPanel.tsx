import { useState } from 'react';
import { BUILTIN_ID, DEFAULT_SOURCE, effectiveSources, slug, ALLOWED_HOSTS, type SourceStatus } from '../../api/templates';
import { t } from '../../i18n';
import { Badge, Button, Icon, IconButton, Input, Switch } from '../../kit';
import { useFile, type TemplateSource } from '../../settings';

/** The list of template sources: our catalog, the Portainer lists, and a form to add one. */
export function SourcesPanel({ status, onReload }: { status: SourceStatus[]; onReload(): void }) {
  const [file, update] = useFile('templates-sources');
  const sources = effectiveSources(file.sources);
  const [url, setUrl] = useState('');
  const [name, setName] = useState('');
  const [err, setErr] = useState('');
  const stat = (id: string) => status.find((s) => s.id === id);

  const save = (list: TemplateSource[]) => void update({ sources: list });

  const add = () => {
    let u: URL;
    try {
      u = new URL(url.trim());
    } catch {
      setErr(t('templates.src.badUrl'));
      return;
    }
    if (u.protocol !== 'https:') return setErr(t('templates.src.https'));
    if (!ALLOWED_HOSTS.includes(u.hostname)) return setErr(t('templates.src.host', { hosts: ALLOWED_HOSTS.join(', ') }));
    if (sources.some((s) => s.url === u.href)) return setErr(t('templates.src.dup'));
    const label = name.trim() || u.pathname.split('/').filter(Boolean).slice(0, 2).join('/') || u.hostname;
    let id = slug(label);
    while (sources.some((s) => s.id === id) || id === BUILTIN_ID) id += '-2';
    save([...sources, { id, name: label, url: u.href, enabled: true }]);
    setUrl('');
    setName('');
    setErr('');
  };

  return (
    <section className="dk-tp-sources" aria-label={t('templates.sources')}>
      <div className="dk-tp-srow">
        <Tile1 />
        <div className="dk-tp-stx">
          <b>{t('templates.src.builtin')}</b>
          <span className="dk-muted">{t('templates.src.builtinNote', { n: stat(BUILTIN_ID)?.count ?? 0 })}</span>
        </div>
        <Badge tone="ok">{t('templates.src.signed')}</Badge>
      </div>
      {sources.map((s) => {
        const st = stat(s.id);
        return (
          <div className="dk-tp-srow" key={s.id}>
            <Tile1 />
            <div className="dk-tp-stx">
              <b>{s.name}</b>
              <span className="dk-muted dk-mono dk-tp-url">{s.url}</span>
              {s.enabled && st?.error && <span className="dk-tp-err">{st.error}</span>}
              {s.enabled && st && !st.error && <span className="dk-muted">{t('templates.src.count', { n: st.count })}</span>}
            </div>
            <Switch checked={s.enabled} onChange={(v) => save(sources.map((x) => (x.id === s.id ? { ...x, enabled: v } : x)))} aria-label={t('templates.src.enable', { name: s.name })} />
            {s.id !== DEFAULT_SOURCE.id && <IconButton icon="trash" label={t('common.remove')} onClick={() => save(sources.filter((x) => x.id !== s.id))} />}
          </div>
        );
      })}
      <div className="dk-tp-add">
        <Input label={t('templates.src.url')} mono value={url} onChange={(e) => { setUrl(e.target.value); setErr(''); }} placeholder="https://raw.githubusercontent.com/…/templates.json" error={err || undefined} hint={t('templates.src.hint', { hosts: ALLOWED_HOSTS.join(', ') })} spellCheck={false} />
        <Input label={t('templates.src.name')} value={name} onChange={(e) => setName(e.target.value)} placeholder={t('templates.src.namePh')} />
        <div className="dk-tp-addbtn"><Button icon="plus" onClick={add} disabled={!url.trim()}>{t('templates.src.add')}</Button></div>
      </div>
      <div className="dk-tp-srow-act"><Button size="sm" icon="refresh" onClick={onReload}>{t('templates.src.reload')}</Button></div>
    </section>
  );
}

function Tile1() {
  return <span className="dk-tp-tile dk-tp-tile--sm hue-file" aria-hidden="true"><Icon name="link" size={16} /></span>;
}
