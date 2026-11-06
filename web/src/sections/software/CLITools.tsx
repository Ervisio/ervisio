import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { call, errorMessage } from '../../api';
import { useT } from '../../i18n';
import { useAccountScope } from '../../lib/accountScope';
import { openApp } from '../../lib/openApp';
import { Badge, Button, ConfirmDialog, Icon } from '../../ui';
import { startJob, useTx } from './tx';
import type { SearchResult } from './types';
import type { OpenItem } from './StoreTab';

interface CLITool {
  id: string;
  title: string;
  docs: string;
  command: string;
  installed: boolean;
  managed: boolean;
  version: string;
  package: string;
  canInstall: boolean;
  userScope: boolean;
  terminalCommand: string;
  terminalShell?: string;
  launcherWarning?: string;
}
interface Props {
  query: string;
  busy: boolean;
  askInstall(p: SearchResult): void;
  onOpen(p: OpenItem): void;
}

export function CLITools({ query, busy, askInstall, onOpen }: Props) {
  const t = useT('software');
  const nav = useNavigate();
  const scope = useAccountScope();
  const phase = useTx().phase;
  const [tools, setTools] = useState<CLITool[] | null>(null);
  const [error, setError] = useState('');
  const [pending, setPending] = useState<{
    tool: CLITool;
    op: 'install' | 'upgrade' | 'remove';
  } | null>(null);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let live = true;
    void call<CLITool[]>('software.cli').then(
      (v) => {
        if (live) {
          setTools(v);
          setError('');
        }
      },
      (e) => {
        if (live) setError(errorMessage(e));
      },
    );
    return () => {
      live = false;
    };
  }, [scope, phase, reload]);
  const shown = (tools ?? []).filter(
    (v) =>
      !query.trim() ||
      `${v.title} ${v.command} ${t(`cli.desc.${v.id}`)}`
        .toLowerCase()
        .includes(query.trim().toLowerCase()),
  );
  const node = tools?.find((v) => v.id === 'node-lts');
  const needsNode =
    !!pending && pending.op !== 'remove' && pending.tool.id !== 'node-lts' && !node?.managed;
  const confirm = () => {
    if (!pending) return;
    const { tool, op } = pending;
    setPending(null);
    const steps = needsNode
      ? [{ op: 'install' as const, packages: ['node-lts'], source: 'cli', scope: 'user' as const }]
      : [];
    startJob({
      title: { key: op === 'upgrade' ? 'update' : op, vars: { name: tool.title } },
      steps: [...steps, { op, packages: [tool.id], source: 'cli', scope: 'user' }],
    });
  };
  const systemPackage = (tool: CLITool): SearchResult => ({
    name: tool.package,
    title: tool.title,
    source: 'repo',
    kind: 'repo',
    description: t(`cli.desc.${tool.id}`),
    version: tool.version,
    installed: tool.installed,
  });
  if (!shown.length && !error) return null;
  return (
    <section aria-label={t('cli.title')}>
      <div className="sw-sec">
        <h3>{t('cli.title')}</h3>
        <span className="sw-muted">{t('cli.subtitle')}</span>
      </div>
      {error && (
        <div className="sw-read-error" role="alert">
          {error}{' '}
          <Button size="sm" variant="ghost" onClick={() => setReload((v) => v + 1)}>
            {t('cli.retry')}
          </Button>
        </div>
      )}
      <div className="sw-apps">
        {shown.map((tool) => (
          <article className="sw-ap sw-cli-card" key={tool.id}>
            <div className="sw-ap-main">
              <span className="sw-cli-icon">
                <Icon name={tool.id === 'node-lts' ? 'code' : 'terminal'} />
              </span>
              <span className="sw-ap-tx">
                <b>{tool.title}</b>
                <small>{t(`cli.desc.${tool.id}`)}</small>
                <span className="sw-ap-m">
                  <Badge tone={tool.installed ? 'ok' : 'neutral'}>
                    {tool.managed
                      ? t('cli.managed')
                      : tool.installed
                        ? t('cli.external')
                        : tool.userScope
                          ? t('cli.user')
                          : t('cli.system')}
                  </Badge>
                  <span className="sw-ver">{tool.version}</span>
                </span>
              </span>
            </div>
            <div className="sw-cli-actions">
              <a href={tool.docs} target="_blank" rel="noreferrer">
                {t('cli.docs')}
              </a>
              {tool.installed && tool.terminalCommand && (
                <Button
                  size="sm"
                  icon="terminal"
                  onClick={() =>
                    openApp(
                      `/terminal?cmd=${encodeURIComponent(tool.terminalCommand)}&launch=${crypto.randomUUID()}${tool.terminalShell ? `&shell=${encodeURIComponent(tool.terminalShell)}` : ''}`,
                      nav,
                    )
                  }
                >
                  {t('cli.open')}
                </Button>
              )}
              {tool.managed ? (
                <>
                  <Button
                    size="sm"
                    disabled={busy}
                    onClick={() => setPending({ tool, op: 'upgrade' })}
                  >
                    {t('update')}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={busy}
                    onClick={() => setPending({ tool, op: 'remove' })}
                  >
                    {t('cli.remove')}
                  </Button>
                </>
              ) : tool.userScope ? (
                <Button
                  size="sm"
                  variant="primary"
                  disabled={busy || !tool.canInstall}
                  onClick={() => setPending({ tool, op: 'install' })}
                >
                  {tool.installed ? t('cli.managedCopy') : t('install.button')}
                </Button>
              ) : tool.installed ? (
                <Button size="sm" onClick={() => onOpen({ ...systemPackage(tool) })}>
                  {t('cli.details')}
                </Button>
              ) : (
                <Button
                  size="sm"
                  variant="primary"
                  disabled={busy || !tool.canInstall}
                  onClick={() => askInstall(systemPackage(tool))}
                >
                  {t('install.button')}
                </Button>
              )}
            </div>
            {tool.launcherWarning && (
              <small className="sw-muted">{t('cli.launcherCollision')}</small>
            )}
            {tool.installed && tool.command && !tool.terminalCommand && (
              <small className="sw-muted">{t('cli.shellUnavailable')}</small>
            )}
            {!tool.canInstall && !tool.installed && (
              <small className="sw-muted">{t('cli.unsupported')}</small>
            )}
          </article>
        ))}
      </div>
      <ConfirmDialog
        open={!!pending}
        onClose={() => setPending(null)}
        onConfirm={confirm}
        title={pending ? t(`cli.confirm.${pending.op}`, { name: pending.tool.title }) : ''}
        description={
          pending?.op === 'remove'
            ? t('cli.removeText')
            : `${t(pending?.tool.id === 'node-lts' ? 'cli.nodeText' : 'cli.installText')}${needsNode ? ` ${t('cli.nodeDependency')}` : ''}${pending?.tool.id === 'claude-code' ? ` ${t('cli.claudeMemory')}` : ''}`
        }
        confirmLabel={pending?.op === 'remove' ? t('cli.remove') : t('install.button')}
        danger={pending?.op === 'remove'}
      />
    </section>
  );
}
