import { useRef, useState } from 'react';
import { useT } from '../i18n';
import { PluginFrame } from '../plugins/PluginFrame';
import type { PluginManifest } from '../plugins/types';
import { Button, Icon } from '../ui';
import './floatingAssistant.css';

export default function FloatingAssistant({ plugin, mode }: { plugin: PluginManifest; mode: 'classic' | 'ervisios' }) {
  const t = useT('shell');
  const [open, setOpen] = useState(false);
  const [visited, setVisited] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const toggle = useRef<HTMLButtonElement>(null);
  const page = plugin.contributes?.pages?.find(page => page.id === 'ai') ?? plugin.contributes?.pages?.[0];
  if (!page) return null;
  const minimize = () => { setOpen(false); toggle.current?.focus(); };
  return <aside className={`floating-assistant${mode === 'ervisios' ? ' floating-assistant--desktop' : ''}`} aria-label={t('assistant.title')}>
    {visited && <section hidden={!open} className={`floating-assistant-panel${expanded ? ' is-expanded' : ''}`} aria-label={t('assistant.title')} onKeyDown={event => {
      if (event.key === 'Escape' && event.target === event.currentTarget) minimize();
    }}>
      <header><Icon name="assistant" /><strong>{t('assistant.title')}</strong><Button variant="ghost" aria-label={t(expanded ? 'assistant.restore' : 'assistant.expand')} onClick={() => setExpanded(value => !value)}><Icon name={expanded ? 'window-restore' : 'window-maximize'} /></Button><Button variant="ghost" aria-label={t('assistant.minimize')} onClick={minimize}><Icon name="close" /></Button></header>
      <PluginFrame plugin={plugin} title={t('assistant.title')} active={open} view={{ kind: 'page', id: page.ervisios?.view ?? page.id, shell: mode, surface: 'assistant' }} style={{ minHeight: 0, minWidth: 0, overflow: 'hidden' }} />
    </section>}
    <button ref={toggle} type="button" className="floating-assistant-toggle" aria-expanded={open} aria-label={t('assistant.toggle')} onClick={() => { setVisited(true); setOpen(value => !value); }}><Icon name="assistant" /><span>AI</span></button>
  </aside>;
}
