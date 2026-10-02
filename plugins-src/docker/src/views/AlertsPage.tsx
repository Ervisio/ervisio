import { useState } from 'react';
import { clearAlertHistory, fireTestAlert, useAlertHistory, useAlertStatus } from '../api/alerts';
import { relativeTime } from '../api/format';
import { t } from '../i18n';
import { Button, EmptyState, Icon, IconButton, Switch, toast } from '../kit';
import { navigate, type RouteProps } from '../router';
import { useFile, type AlertRule } from '../settings';
import { PageHeader } from '../ui/PageHeader';
import { RuleForm } from './alerts/RuleForm';
import { KIND_ICON, defaultRule, scopeText, summary, usualRules } from './alerts/rules';

/** Route { view: 'alerts' }: rules with switches and an inline form, and the recent alerts. */
export function AlertsPage(_props: RouteProps<'alerts'>) {
  const [file, update, loaded] = useFile('alerts');
  const history = useAlertHistory();
  const status = useAlertStatus();
  const [editing, setEditing] = useState<string | null>(null); // rule id, or 'new'
  const [draft, setDraft] = useState<AlertRule | null>(null);
  const [removing, setRemoving] = useState<AlertRule | null>(null);
  const rules = file.rules;

  const save = async (next: AlertRule[]) => {
    try {
      await update({ rules: next });
    } catch (e) {
      toast.err(t('alerts.saveFailed'), (e as Error).message);
    }
  };
  const submit = async (r: AlertRule) => {
    const exists = rules.some((x) => x.id === r.id);
    await save(exists ? rules.map((x) => (x.id === r.id ? r : x)) : [...rules, r]);
    setEditing(null);
    setDraft(null);
  };
  const on = rules.filter((r) => r.enabled).length;
  const kindColor = (k: AlertRule['kind']) => (k === 'cpu' || k === 'memory' || k === 'disk' ? 'hue-log' : 'hue-svc');

  return (
    <div className="dk-al">
      <PageHeader
        icon="bell"
        hue="log"
        title={t('alerts.title')}
        subtitle={t('alerts.sub')}
        actions={
          <>
            <Button icon="zap" onClick={fireTestAlert}>{t('alerts.test')}</Button>
            <Button variant="primary" icon="plus" onClick={() => { setDraft(defaultRule('stopped')); setEditing('new'); }}>{t('alerts.add')}</Button>
          </>
        }
      />

      <div className={`dk-al-note ${status.running ? 'dk-al-note--on' : ''}`}>
        <span className="dk-al-note-ic"><Icon name="info" /></span>
        <div>
          <b>{t('alerts.notice.title')}</b>
          <p>{t('alerts.notice.text')}</p>
        </div>
        <span className="dk-al-state"><span className={`dk-dot dk-dot--${status.running ? 'ok' : 'warn'}`} />{t(status.running ? 'alerts.notice.running' : 'alerts.notice.idle')}</span>
      </div>

      <section className="dk-al-sec">
        <div className="dk-al-sh">
          <h2>{t('alerts.rules')}</h2>
          {rules.length > 0 && <span className="dk-muted">{t('alerts.rules.count', { n: on })}</span>}
        </div>

        {editing === 'new' && draft && <RuleForm initial={draft} isNew onSave={submit} onCancel={() => { setEditing(null); setDraft(null); }} />}

        {loaded && rules.length === 0 && editing !== 'new' && (
          <EmptyState
            icon="bell"
            hue="log"
            title={t('alerts.empty.title')}
            text={t('alerts.empty.text')}
            action={<Button variant="primary" icon="plus" onClick={() => void save(usualRules())}>{t('alerts.empty.usual')}</Button>}
          />
        )}

        <div className="dk-al-list">
          {rules.map((r) =>
            editing === r.id ? (
              <RuleForm key={r.id} initial={r} isNew={false} onSave={submit} onCancel={() => setEditing(null)} />
            ) : (
              <div key={r.id} className={`dk-al-row ${r.enabled ? '' : 'dk-al-row--off'}`}>
                <span className={`dk-al-ic ${kindColor(r.kind)}`}><Icon name={KIND_ICON[r.kind]} /></span>
                <div className="dk-al-tx">
                  <b>{t(`alerts.kind.${r.kind}`)}</b>
                  <span>{summary(r)}</span>
                </div>
                <span className="dk-tag dk-al-scope-tag">{scopeText(r)}</span>
                <Switch checked={r.enabled} aria-label={`${t('alerts.on')}: ${t(`alerts.kind.${r.kind}`)}`} onChange={(v) => void save(rules.map((x) => (x.id === r.id ? { ...x, enabled: v } : x)))} />
                <IconButton icon="edit" label={t('alerts.edit')} size="sm" onClick={() => { setDraft(null); setEditing(r.id); }} />
                {removing?.id === r.id ? (
                  <span className="dk-al-ask">
                    <b>{t('alerts.deleteConfirm.title')}</b>
                    <Button size="sm" variant="danger" onClick={() => { void save(rules.filter((x) => x.id !== r.id)); setRemoving(null); }}>{t('common.remove')}</Button>
                    <Button size="sm" variant="ghost" onClick={() => setRemoving(null)}>{t('common.cancel')}</Button>
                  </span>
                ) : (
                  <IconButton icon="trash" label={t('alerts.delete')} size="sm" onClick={() => setRemoving(r)} />
                )}
              </div>
            ),
          )}
        </div>
      </section>

      <section className="dk-al-sec">
        <div className="dk-al-sh">
          <h2>{t('alerts.recent')}</h2>
          {history.length > 0 && <Button variant="ghost" size="sm" icon="trash" onClick={() => void clearAlertHistory()}>{t('alerts.recent.clear')}</Button>}
        </div>
        {history.length === 0 ? (
          <p className="dk-al-none">{t('alerts.recent.none')}</p>
        ) : (
          <div className="dk-al-list">
            {history.map((h) => (
              <div key={h.id} className="dk-al-row">
                <span className={`dk-al-ic ${h.kind !== 'test' ? kindColor(h.kind) : 'hue-file'}`}><Icon name={h.kind === 'test' ? 'bell' : KIND_ICON[h.kind]} /></span>
                <div className="dk-al-tx">
                  <b>{h.title}</b>
                  <span>{h.detail}</span>
                </div>
                <span className="dk-muted dk-al-when" title={new Date(h.at).toLocaleString()}>{relativeTime(new Date(h.at).toISOString())}</span>
                {h.containerId && <Button size="sm" variant="secondary" icon="externallink" onClick={() => navigate({ view: 'container', id: h.containerId! })}>{t('alerts.recent.open')}</Button>}
              </div>
            ))}
          </div>
        )}
      </section>

    </div>
  );
}

