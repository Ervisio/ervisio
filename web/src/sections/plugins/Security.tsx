import { useT } from '../../i18n';
import { Badge, Switch } from '../../ui';
import { folderOf, httpApis, runsRoot } from './caps';
import { Tile } from './Parts';
import type { PluginInfo } from './types';

interface Props {
  plugins: PluginInfo[];
  busyId: string;
  onToggle(p: PluginInfo, on: boolean): void;
}

const Dot = ({ on, hot, label, detail }: { on: boolean; hot?: boolean; label: string; detail?: string }) => (
  <span className={`plugins-dot${on ? ' is-on' : ''}${on && hot ? ' is-hot' : ''}`} role="img" aria-label={detail ? `${label}. ${detail}` : label} title={detail} />
);

export function Security({ plugins, busyId, onToggle }: Props) {
  const t = useT('plugins');
  const enabled = plugins.filter((p) => p.enabled).length;
  const root = plugins.filter((p) => runsRoot(p.capabilities)).length;
  const unsigned = plugins.filter((p) => !p.verified).length;
  const updates = plugins.filter((p) => p.updateAvailable).length;
  const cards: { n: number; text: string; hue: string }[] = [
    { n: plugins.length, text: t('sum.installed', { enabled }), hue: 'plg' },
    { n: root, text: t('sum.root'), hue: 'svc' },
    { n: unsigned, text: t('sum.unsigned'), hue: 'log' },
    { n: updates, text: t('sum.updates'), hue: 'term' },
  ];
  const cols = [t('col.root'), t('col.sockets'), t('col.pty'), t('col.read'), t('col.write'), t('col.network'), t('col.pages')];
  return (
    <div className="plugins-sec">
      <div className="plugins-sum">
        {cards.map((c) => (
          <div key={c.text} className={`plugins-sum-c hue-${c.hue}`}>
            <span className="plugins-sum-n">{c.n}</span>
            <small>{c.text}</small>
          </div>
        ))}
      </div>
      <div className="plugins-gridwrap">
        <table className="plugins-table">
          <thead>
            <tr>
              <th>{t('col.plugin')}</th>
              <th>{t('col.signature')}</th>
              {cols.map((c) => <th key={c} className="cc">{c}</th>)}
              <th>{t('col.on')}</th>
            </tr>
          </thead>
          <tbody>
            {plugins.map((p) => {
              const c = p.capabilities;
              const http = httpApis(c);
              const pty = c.commands.filter((x) => x.pty);
              const paths = (l: typeof c.files.read) => l.map((f) => (folderOf(f).admin ? `${folderOf(f).path} (root)` : folderOf(f).path)).join(', ');
              // Each dot's tooltip lists what it stands for: sockets with their HTTP rule counts, terminal commands, folders.
              const flags: [boolean, string?][] = [
                [runsRoot(c)],
                [c.sockets.length > 0 || http.length > 0, [...http.map((h) => `${h.socket} (${h.rules?.length ?? 0} HTTP${h.admin ? ', root' : ''})`), ...c.sockets].join(', ')],
                [pty.length > 0, pty.map((x) => x.name).join(', ')],
                [c.files.read.length > 0, paths(c.files.read)],
                [c.files.write.length > 0, paths(c.files.write)],
                [c.network.length > 0, c.network.join(', ')],
                [p.contributes.pages.length + p.contributes.widgets.length > 0],
              ];
              return (
                <tr key={p.id} className={!p.verified ? 'is-warn' : ''}>
                  <td>
                    <div className="plugins-nm">
                      <Tile icon={p.icon} color={p.color} size="sm" />
                      <div><b>{p.name}</b><small>v{p.version}</small></div>
                    </div>
                  </td>
                  <td>
                    {p.verified ? <Badge tone="ok">{t('signed')}</Badge> : p.signed ? <Badge tone="err"><span title={p.signatureError}>{t('invalidSignature')}</span></Badge> : <Badge tone="warn">{t('unsigned')}</Badge>}
                  </td>
                  {flags.map(([f, detail], i) => (
                    <td key={i} className="cc"><Dot on={f} hot={i === 0} label={`${cols[i]}: ${f ? t('yes') : t('no')}`} detail={f ? detail || undefined : undefined} /></td>
                  ))}
                  <td><Switch checked={p.enabled} disabled={busyId === p.id || !!p.blocked || !!p.incompatible || !!p.error} onChange={(on) => onToggle(p, on)} aria-label={t('toggle', { name: p.name })} /></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
