import { useT } from '../../i18n';
import { Badge, Switch } from '../../ui';
import { runsRoot } from './caps';
import { Tile } from './Parts';
import type { PluginInfo } from './types';

interface Props {
  plugins: PluginInfo[];
  busyId: string;
  onToggle(p: PluginInfo, on: boolean): void;
}

const Dot = ({ on, hot, label }: { on: boolean; hot?: boolean; label: string }) => (
  <span className={`plugins-dot${on ? ' is-on' : ''}${on && hot ? ' is-hot' : ''}`} role="img" aria-label={label} />
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
  const cols = [t('col.root'), t('col.sockets'), t('col.read'), t('col.write'), t('col.network'), t('col.pages')];
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
              const flags = [runsRoot(c), c.sockets.length > 0, c.files.read.length > 0, c.files.write.length > 0, c.network.length > 0, p.contributes.pages.length + p.contributes.widgets.length > 0];
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
                  {flags.map((f, i) => (
                    <td key={i} className="cc"><Dot on={f} hot={i === 0} label={`${cols[i]}: ${f ? t('yes') : t('no')}`} /></td>
                  ))}
                  <td><Switch checked={p.enabled} disabled={busyId === p.id || !!p.blocked || !!p.error} onChange={(on) => onToggle(p, on)} aria-label={t('toggle', { name: p.name })} /></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
