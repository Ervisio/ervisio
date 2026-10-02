import { useState } from 'react';
import { useMemo } from 'react';
import { containers } from '../../api/resources';
import { initialValues, randomSecret, type Template, type TemplateVar } from '../../api/templates';
import { usedHostPorts, validPort } from '../create/model';
import { t } from '../../i18n';
import { Hint } from '../create/parts';
import { IconButton, Input, Switch } from '../../kit';

export interface FormState {
  name: string;
  values: Record<string, string>;
}

export const initialForm = (tpl: Template, defaultName: string): FormState => ({ name: defaultName, values: initialValues(tpl.variables) });

/** Problems that stop the install, as i18n keys with variables. */
export function formProblems(tpl: Template, f: FormState): Record<string, string> {
  const p: Record<string, string> = {};
  for (const v of tpl.variables) {
    const val = (f.values[v.name] ?? '').trim();
    if (v.required && !val && v.type !== 'bool') p[v.name] = 'templates.err.required';
    else if (v.type === 'port' && val && !validPort(val)) p[v.name] = 'templates.err.port';
    else if (v.type === 'path' && val && !val.startsWith('/')) p[v.name] = 'templates.err.path';
  }
  return p;
}

/** The variables of a template as a form. */
export function InstallFields({ tpl, form, setForm, errors, showErrors }: { tpl: Template; form: FormState; setForm(f: FormState): void; errors: Record<string, string>; showErrors: boolean }) {
  const { data: list } = containers.use();
  const used = useMemo(() => usedHostPorts(list), [list]);
  const [shown, setShown] = useState<Set<string>>(new Set());
  const set = (name: string, value: string) => setForm({ ...form, values: { ...form.values, [name]: value } });

  const field = (v: TemplateVar) => {
    const val = form.values[v.name] ?? '';
    const err = showErrors && errors[v.name] ? t(errors[v.name]) : undefined;
    if (v.type === 'bool') {
      return (
        <div key={v.name} className="dk-tp-bool">
          <Switch checked={val === 'true'} onChange={(on) => set(v.name, on ? 'true' : 'false')} label={v.label} />
          {v.hint && <span className="dk-muted">{v.hint}</span>}
        </div>
      );
    }
    const by = v.type === 'port' && validPort(val.trim()) ? used.get(`${val.trim()}/tcp`) ?? used.get(`${val.trim()}/udp`) : undefined;
    const secret = v.type === 'password' && !shown.has(v.name);
    return (
      <div key={v.name} className="dk-tp-fld">
        <Input
          label={v.label}
          mono={v.type !== 'text' || /^[A-Z_]+$/.test(v.name)}
          type={secret ? 'password' : 'text'}
          inputMode={v.type === 'port' ? 'numeric' : undefined}
          value={val}
          onChange={(e) => set(v.name, e.target.value)}
          error={err}
          hint={!err ? v.hint : undefined}
          autoComplete="off"
          spellCheck={false}
          end={
            v.type === 'password' ? (
              <>
                <IconButton icon={shown.has(v.name) ? 'eyeoff' : 'eye'} label={shown.has(v.name) ? t('create.env.hide') : t('create.env.show')} size="sm" onClick={() => setShown((s) => { const n = new Set(s); if (n.has(v.name)) n.delete(v.name); else n.add(v.name); return n; })} />
                <IconButton icon="refresh" label={t('templates.generate')} size="sm" onClick={() => set(v.name, randomSecret())} />
              </>
            ) : undefined
          }
        />
        {v.type === 'port' && validPort(val.trim()) && (by ? <Hint tone="warn" icon="alert">{t('create.port.conflict', { port: val.trim(), name: by })}</Hint> : <Hint tone="ok" icon="check">{t('templates.port.free')}</Hint>)}
      </div>
    );
  };

  return (
    <div className="dk-tp-form">
      <Input label={tpl.type === 'stack' ? t('templates.stackName') : t('create.name')} mono value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} spellCheck={false} autoCapitalize="off" error={showErrors && errors.__name ? t(errors.__name) : undefined} hint={tpl.type === 'stack' ? t('templates.stackName.hint') : undefined} />
      {tpl.variables.length > 0 && <h4>{t('templates.settings')}</h4>}
      <div className="dk-tp-vars">{tpl.variables.map(field)}</div>
    </div>
  );
}
