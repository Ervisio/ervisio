import { useState } from 'react';
import { formatDuration, relativeTime, shortId } from '../../api/format';
import { portUrl } from '../../api/model';
import type { ContainerInspect } from '../../api/types';
import { t } from '../../i18n';
import { Badge, IconButton, Icon, toast } from '../../kit';
import { openUrl } from '../../ui/openUrl';
import { LiveCharts } from './LiveCharts';
import { copyText, looksSecret, useNow } from './util';

const fmtDate = (iso: string): string => {
  const d = new Date(iso);
  return isFinite(d.getTime()) ? d.toLocaleString() : '–';
};

const joinCmd = (a?: string[] | null): string => (a && a.length ? a.map((x) => (/\s/.test(x) ? JSON.stringify(x) : x)).join(' ') : '');

export function Overview({ id, inspect, running }: { id: string; inspect: ContainerInspect; running: boolean }) {
  return (
    <div className="dk-c-ov">
      <LiveCharts id={id} running={running} inspect={inspect} height={70} />
      <div className="dk-c-two">
        <Facts inspect={inspect} />
        <div className="dk-card">
          <Ports inspect={inspect} />
          <Mounts inspect={inspect} />
        </div>
      </div>
      <Env inspect={inspect} />
      <div className="dk-c-two">
        <Networks inspect={inspect} />
        <Labels inspect={inspect} />
      </div>
    </div>
  );
}

function Facts({ inspect: c }: { inspect: ContainerInspect }) {
  useNow(30000);
  const [copied, setCopied] = useState(false);
  const policy = c.HostConfig.RestartPolicy;
  const policyText = policy?.Name ? (policy.Name === 'on-failure' && policy.MaximumRetryCount ? `on-failure:${policy.MaximumRetryCount}` : policy.Name) : t('container.policyNo');
  const health = c.State.Health;
  const lastCheck = health?.Log?.[health.Log.length - 1];
  const cmd = joinCmd(c.Config.Cmd) || joinCmd([c.Path, ...(c.Args ?? [])]);
  const ep = joinCmd(c.Config.Entrypoint);
  const rows: [string, React.ReactNode][] = [
    [t('container.f.id'), (
      <span className="dk-c-inline">
        <span className="dk-mono" title={c.Id}>{shortId(c.Id)}</span>
        <IconButton size="sm" variant="ghost" icon={copied ? 'check' : 'copy'} label={t('container.f.copyId')} onClick={async () => {
          if (await copyText(c.Id)) { setCopied(true); setTimeout(() => setCopied(false), 1500); } else toast.err(t('container.copyFail'));
        }} />
      </span>
    )],
    [t('container.f.created'), `${fmtDate(c.Created)} (${relativeTime(c.Created)})`],
    [t('container.f.started'), c.State.Running && c.State.StartedAt ? `${relativeTime(c.State.StartedAt)}, ${t('container.up', { time: formatDuration((Date.now() - Date.parse(c.State.StartedAt)) / 1000) })}` : c.State.FinishedAt && !c.State.FinishedAt.startsWith('0001') ? t('container.stoppedAgo', { when: relativeTime(c.State.FinishedAt), code: c.State.ExitCode }) : '–'],
    [t('container.f.policy'), c.RestartCount ? `${policyText}, ${t('container.restarts', { n: c.RestartCount })}` : policyText],
    [t('container.f.command'), cmd ? <span className="dk-mono dk-c-wrapany">{cmd}</span> : '–'],
    [t('container.f.entrypoint'), ep ? <span className="dk-mono dk-c-wrapany">{ep}</span> : '–'],
  ];
  if (c.Config.WorkingDir) rows.push([t('container.f.workdir'), <span className="dk-mono">{c.Config.WorkingDir}</span>]);
  if (c.Config.User) rows.push([t('container.f.user'), <span className="dk-mono">{c.Config.User}</span>]);
  if (c.State.Running) rows.push([t('container.f.pid'), String(c.State.Pid)]);
  if (c.State.OOMKilled) rows.push([t('container.f.oom'), <Badge tone="err">{t('container.oom')}</Badge>]);
  if (c.State.Error) rows.push([t('container.f.error'), <span className="dk-c-err">{c.State.Error}</span>]);
  return (
    <div className="dk-card">
      <h3>{t('container.facts')}</h3>
      <dl className="dk-c-kv">
        {rows.map(([k, v], i) => (
          <div key={i} className="dk-c-kv-r"><dt>{k}</dt><dd>{v}</dd></div>
        ))}
      </dl>
      {health && (
        <div className="dk-c-health">
          <div className="dk-c-health-h">
            <b>{t('container.f.health')}</b>
            <Badge tone={health.Status === 'healthy' ? 'ok' : health.Status === 'unhealthy' ? 'err' : 'warn'}>{t(`health.${health.Status}`)}</Badge>
            {health.FailingStreak > 0 && <span className="dk-muted">{t('container.failingStreak', { n: health.FailingStreak })}</span>}
          </div>
          {lastCheck ? (
            <div className="dk-c-check">
              <span className="dk-muted">{t('container.lastCheck', { when: relativeTime(lastCheck.End), code: lastCheck.ExitCode })}</span>
              {lastCheck.Output.trim() && <pre>{lastCheck.Output.trim().slice(0, 600)}</pre>}
            </div>
          ) : <span className="dk-muted">{t('container.noChecks')}</span>}
        </div>
      )}
    </div>
  );
}

function Ports({ inspect: c }: { inspect: ContainerInspect }) {
  const entries = Object.entries(c.NetworkSettings.Ports ?? {});
  const rows: { host?: number; ip?: string; container: number; proto: string }[] = [];
  for (const [k, binds] of entries) {
    const [p, proto] = k.split('/');
    const container = Number(p);
    if (binds?.length) {
      const seen = new Set<string>();
      for (const b of binds) {
        const key = `${b.HostPort}`;
        if (seen.has(key)) continue;
        seen.add(key);
        rows.push({ host: Number(b.HostPort), ip: b.HostIp, container, proto });
      }
    } else rows.push({ container, proto });
  }
  return (
    <>
      <h3>{t('container.ports')}</h3>
      {rows.length === 0 ? (
        <p className="dk-muted">{c.HostConfig.NetworkMode === 'host' ? t('container.portsHost') : t('container.portsNone')}</p>
      ) : rows.map((r, i) => (
        <div key={i} className="dk-c-row">
          {r.host ? (
            <>
              {r.proto === 'tcp' ? (
                <button type="button" className="dk-port" title={t('containers.port', { port: r.host })} onClick={() => openUrl(portUrl({ host: r.host!, container: r.container, proto: r.proto }))}>
                  {r.host}<Icon name="externallink" />
                </button>
              ) : <span className="dk-tag">{r.host}</span>}
              <Icon name="right" />
              <span className="dk-mono">{r.container}/{r.proto}</span>
              <span className="dk-muted dk-c-row-end">{r.ip || '0.0.0.0'}</span>
            </>
          ) : (
            <>
              <span className="dk-mono">{r.container}/{r.proto}</span>
              <span className="dk-muted dk-c-row-end">{t('container.notPublished')}</span>
            </>
          )}
        </div>
      ))}
    </>
  );
}

function Mounts({ inspect: c }: { inspect: ContainerInspect }) {
  return (
    <>
      <h3 className="dk-c-h3-gap">{t('container.mounts')}</h3>
      {c.Mounts.length === 0 ? <p className="dk-muted">{t('container.mountsNone')}</p> : c.Mounts.map((m, i) => (
        <div key={i} className="dk-c-row">
          <span className={`dk-c-mt dk-c-mt--${m.Type}`}>{m.Type}</span>
          <span className="dk-mono dk-c-src" title={m.Name || m.Source}>{m.Name || m.Source}</span>
          <Icon name="right" />
          <span className="dk-mono dk-c-dst" title={m.Destination}>{m.Destination}</span>
          <span className="dk-tag dk-c-row-end">{m.RW ? 'rw' : 'ro'}</span>
        </div>
      ))}
    </>
  );
}

function Env({ inspect: c }: { inspect: ContainerInspect }) {
  const [shown, setShown] = useState<Set<string>>(new Set());
  const env = (c.Config.Env ?? []).map((e) => {
    const i = e.indexOf('=');
    return i < 0 ? { k: e, v: '' } : { k: e.slice(0, i), v: e.slice(i + 1) };
  });
  const toggle = (k: string) => setShown((s) => { const n = new Set(s); if (n.has(k)) n.delete(k); else n.add(k); return n; });
  return (
    <div className="dk-card">
      <h3>{t('container.env')}<span className="dk-muted dk-c-h3-n">{env.length}</span></h3>
      {env.length === 0 ? <p className="dk-muted">{t('container.envNone')}</p> : env.map((e, i) => {
        const secret = looksSecret(e.k);
        const hidden = secret && !shown.has(e.k);
        return (
          <div key={`${e.k}-${i}`} className="dk-c-row">
            <span className="dk-mono dk-c-k">{e.k}</span>
            <span className={`dk-mono dk-c-v${hidden ? ' dk-c-masked' : ''}`}>{hidden ? '••••••••••' : e.v}</span>
            {secret && <IconButton size="sm" variant="ghost" icon={hidden ? 'eye' : 'eyeoff'} label={hidden ? t('container.reveal') : t('container.hide')} onClick={() => toggle(e.k)} />}
          </div>
        );
      })}
    </div>
  );
}

function Networks({ inspect: c }: { inspect: ContainerInspect }) {
  const nets = Object.entries(c.NetworkSettings.Networks ?? {});
  return (
    <div className="dk-card">
      <h3>{t('container.networks')}</h3>
      {nets.length === 0 ? <p className="dk-muted">{t('container.networksNone')}</p> : nets.map(([name, n]) => (
        <div key={name} className="dk-c-row dk-c-row--wrap">
          <b>{name}</b>
          <span className="dk-mono">{n.IPAddress || '–'}</span>
          {n.Gateway && <span className="dk-muted">{t('container.gateway', { ip: n.Gateway })}</span>}
          {n.MacAddress && <span className="dk-muted dk-c-row-end dk-mono">{n.MacAddress}</span>}
        </div>
      ))}
    </div>
  );
}

function Labels({ inspect: c }: { inspect: ContainerInspect }) {
  const labels = Object.entries(c.Config.Labels ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return (
    <div className="dk-card">
      <details className="dk-c-fold">
        <summary><h3>{t('container.labels')}<span className="dk-muted dk-c-h3-n">{labels.length}</span></h3><Icon name="chevron" /></summary>
        <div className="dk-c-fold-b">
          {labels.length === 0 ? <p className="dk-muted">{t('container.labelsNone')}</p> : labels.map(([k, v]) => (
            <div key={k} className="dk-c-row"><span className="dk-mono dk-c-k" title={k}>{k}</span><span className="dk-mono dk-c-v" title={v}>{v}</span></div>
          ))}
        </div>
      </details>
    </div>
  );
}
