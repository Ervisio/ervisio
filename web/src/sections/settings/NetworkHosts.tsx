import { useCallback, useEffect, useState } from 'react';
import { ApiError } from '../../api';
import { useT } from '../../i18n';
import { formatDate } from '../../lib/format';
import { Badge, ConfirmDialog, IconButton, Skeleton, toast } from '../../ui';
import { hostRevoke, hostsList, type ApprovedHost } from './envs';
import './envs.css';

/** Settings > Plugin policy: hosts an administrator approved for plugins with capabilities.network.userHosts. */
export function NetworkHostsBlock({ disabled }: { disabled?: boolean }) {
  const t = useT('envs');
  const [list, setList] = useState<ApprovedHost[] | null>(null);
  const [failed, setFailed] = useState('');
  const [revoking, setRevoking] = useState<ApprovedHost | null>(null);
  const load = useCallback(async () => {
    try {
      setList((await hostsList()).approved ?? []);
      setFailed('');
    } catch (e) {
      setFailed(e instanceof ApiError || e instanceof Error ? e.message : String(e));
    }
  }, []);
  useEffect(() => {
    if (!disabled) void load();
  }, [load, disabled]);
  if (failed) return <div className="env-empty"><small>{failed}</small></div>;
  if (!list) return <Skeleton lines={2} />;
  return (
    <>
      {list.length === 0 && <div className="env-empty"><small>{t('hosts.none')}</small></div>}
      {list.map((a) => (
        <div className="st-host" key={`${a.plugin}/${a.host}`}>
          <span className="dot" style={{ background: a.scheme === 'http' ? 'var(--warn)' : 'var(--ok)' }} />
          <div className="grow">
            <b className="mono">{a.host}</b>
            <small>{t('hosts.for', { plugin: a.plugin })} · {t('hosts.by', { by: a.by, date: formatDate(a.at) })}</small>
          </div>
          <Badge tone={a.scheme === 'http' ? 'warn' : 'ok'}>{a.scheme === 'http' ? t('network.http') : t('network.https')}</Badge>
          <IconButton icon="trash" label={t('hosts.revoke', { host: a.host })} disabled={disabled} onClick={() => setRevoking(a)} />
        </div>
      ))}
      <ConfirmDialog
        open={!!revoking}
        onClose={() => setRevoking(null)}
        onConfirm={async () => {
          if (!revoking) return;
          await hostRevoke(revoking.plugin, revoking.host);
          toast.ok(t('hosts.revoked', { host: revoking.host }));
          await load();
        }}
        title={t('hosts.revokeTitle', { host: revoking?.host ?? '' })}
        description={t('hosts.revokeText', { plugin: revoking?.plugin ?? '' })}
        confirmLabel={t('hosts.revokeConfirm')}
        icon="trash"
      />
    </>
  );
}
