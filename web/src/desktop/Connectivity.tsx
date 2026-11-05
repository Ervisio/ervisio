import { useCallback, useEffect, useRef, useState } from 'react';
import { call, errorMessage, useSession } from '../api';
import { useT } from '../i18n';
import { Button, Icon, Skeleton, Switch } from '../ui';
import './connectivity.css';

type Radio = 'wifi' | 'bluetooth';
export interface RadioStatus {
  present: boolean;
  supported: boolean;
  enabled: boolean | null;
  state: 'on' | 'off' | 'unknown' | 'unavailable';
  reason?:
    | 'no_hardware'
    | 'missing_tool'
    | 'permission_denied'
    | 'query_failed'
    | 'hardware_blocked';
  detail?: string;
  connection?: string;
  ssid?: string;
  devices: { id: string; name: string }[];
}
export interface HostConnectivity {
  source: 'host';
  platform: 'linux' | 'windows';
  wifi: RadioStatus;
  bluetooth: RadioStatus;
  interfaces?: {
    id: string;
    name: string;
    type: 'ethernet' | 'wifi' | 'other';
    state: 'up' | 'down';
    addresses: string[];
    connection?: string;
  }[];
}

/** The quick panel mounts this component while open. These are server radios, never the browser device. */
export function Connectivity() {
  const t = useT('shell');
  const { host, session } = useSession();
  const canManage = !!session && (!!session.isRoot || session.isAdmin || session.canSudo);
  const [data, setData] = useState<HostConnectivity | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<Radio | null>(null);
  const [error, setError] = useState('');
  const mounted = useRef(false);
  const controller = useRef<AbortController | null>(null);
  const sequence = useRef(0);
  const changing = useRef(false);
  // Retain active virtual links while keeping inactive tunnels from burying
  // Ethernet and Wi-Fi details. The API retains the complete interface list.
  const interfaces =
    data?.interfaces?.filter(
      (network) => network.state === 'up' || network.type === 'ethernet' || network.type === 'wifi',
    ) ?? [];

  const load = useCallback(async () => {
    if (changing.current) return;
    const request = ++sequence.current;
    setLoading(true);
    try {
      const value = await call<HostConnectivity>(
        'system.connectivity',
        {},
        { noUnlock: true, signal: controller.current?.signal },
      );
      if (mounted.current && request === sequence.current) {
        setData(value);
        setError('');
      }
    } catch (failure) {
      if (mounted.current && request === sequence.current) setError(errorMessage(failure));
    } finally {
      if (mounted.current && request === sequence.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    mounted.current = true;
    controller.current = new AbortController();
    void load();
    const timer = window.setInterval(() => {
      if (!document.hidden) void load();
    }, 15_000);
    return () => {
      mounted.current = false;
      sequence.current += 1;
      controller.current?.abort();
      window.clearInterval(timer);
    };
  }, [load]);

  const change = async (radio: Radio, enabled: boolean) => {
    const current = data?.[radio];
    if (changing.current || !canManage || !current?.supported || current.enabled === null) return;
    changing.current = true;
    const request = ++sequence.current;
    setBusy(radio);
    setError('');
    try {
      const value = await call<HostConnectivity>(
        'system.connectivity.set',
        { radio, enabled },
        { admin: true, signal: controller.current?.signal },
      );
      if (mounted.current && request === sequence.current) setData(value);
    } catch (failure) {
      if (mounted.current && request === sequence.current) setError(errorMessage(failure));
    } finally {
      changing.current = false;
      if (mounted.current && request === sequence.current) {
        setBusy(null);
        setLoading(false);
      }
    }
  };

  return (
    <section className="eos-connectivity" aria-labelledby="eos-connectivity-title">
      <div className="eos-connectivity-heading">
        <strong id="eos-connectivity-title">{t('desktop.connectivity.title')}</strong>
        <Button
          variant="ghost"
          size="sm"
          icon="refresh"
          iconOnly
          aria-label={t('desktop.connectivity.refresh')}
          loading={loading}
          disabled={!!busy}
          onClick={() => void load()}
        />
      </div>
      <p className="eos-connectivity-hint">
        {t('desktop.connectivity.server', { name: host?.hostname ?? t('host.thisMachine') })}
      </p>
      {!data && loading ? (
        <div className="eos-connectivity-loading" aria-busy="true">
          <Skeleton height={54} />
          <Skeleton height={54} />
        </div>
      ) : (
        data &&
        (['wifi', 'bluetooth'] as const).map((radio) => {
          const state = data[radio];
          const label = t(`desktop.connectivity.${radio}`);
          const status =
            busy === radio
              ? t('desktop.connectivity.changing')
              : t(`desktop.connectivity.${state.state}`);
          const disabled = !!busy || !canManage || !state.supported || state.enabled === null;
          return (
            <div className="eos-connectivity-radio" key={radio}>
              <span className={`eos-connectivity-icon${state.enabled === true ? ' is-on' : ''}`}>
                <Icon name={radio === 'wifi' ? 'wifi' : 'link'} size={19} />
              </span>
              <div className="eos-connectivity-text">
                <strong>{label}</strong>
                <span aria-live="polite">{status}</span>
                {state.ssid && (
                  <small title={state.ssid}>
                    {t('desktop.connectivity.ssid', { name: state.ssid })}
                  </small>
                )}
                {state.connection && state.connection !== state.ssid && (
                  <small title={state.connection}>
                    {t('desktop.connectivity.connection', { name: state.connection })}
                  </small>
                )}
                {state.devices?.length > 0 && (
                  <small title={state.devices.map((device) => device.name).join(', ')}>
                    {state.devices.map((device) => device.name).join(', ')}
                  </small>
                )}
              </div>
              <Switch
                aria-label={label}
                checked={state.enabled === true}
                disabled={disabled}
                onChange={(enabled) => void change(radio, enabled)}
              />
              {state.reason && (
                <p className="eos-connectivity-reason">
                  {t(`desktop.connectivity.${state.reason}`)}
                </p>
              )}
              {!canManage && state.supported && (
                <p className="eos-connectivity-reason">{t('desktop.connectivity.adminRequired')}</p>
              )}
              {state.detail && (
                <details className="eos-connectivity-detail">
                  <summary>{t('desktop.connectivity.details')}</summary>
                  <p>{state.detail}</p>
                </details>
              )}
            </div>
          );
        })
      )}
      {interfaces.length > 0 && (
        <div className="eos-connectivity-links" aria-label={t('desktop.connectivity.interfaces')}>
          {interfaces.map((network) => (
            <div className="eos-connectivity-link" key={network.id}>
              <Icon name={network.type === 'wifi' ? 'wifi' : 'net'} size={16} />
              <div>
                <strong>
                  {t(
                    `desktop.connectivity.${network.type === 'ethernet' ? 'ethernet' : network.type === 'wifi' ? 'wireless' : 'network'}`,
                  )}
                </strong>
                <span>
                  {network.name}{' '}
                  <b>
                    {t(`desktop.connectivity.${network.state === 'up' ? 'linkUp' : 'linkDown'}`)}
                  </b>
                </span>
                {network.connection && <small>{network.connection}</small>}
                {network.addresses.length > 0 && <code>{network.addresses.join(', ')}</code>}
              </div>
            </div>
          ))}
        </div>
      )}
      {error && (
        <div className="eos-connectivity-error" role="alert">
          <strong>{t('desktop.connectivity.failed')}</strong>
          <p>{error}</p>
          <Button size="sm" onClick={() => void load()}>
            {t('desktop.retry')}
          </Button>
        </div>
      )}
    </section>
  );
}
