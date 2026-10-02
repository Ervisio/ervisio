import { useState } from 'react';
import { t } from '../../i18n';
import { Button, IconButton, Input, Textarea } from '../../kit';
import { Hint, removeAt, replaceAt, Section, type StepProps } from './parts';
import { isSecretKey, kv, parseDotenv, type Problems } from './model';

export function EnvStep({ spec, patch, showErrors, problems }: StepProps & { problems: Problems }) {
  const [paste, setPaste] = useState(false);
  const [text, setText] = useState('');
  const [shown, setShown] = useState<Set<string>>(new Set());

  const apply = () => {
    const parsed = parseDotenv(text);
    const merged = spec.env.filter((e) => e.key.trim() || e.value);
    for (const p of parsed) {
      const at = merged.findIndex((e) => e.key === p.key);
      if (at >= 0) merged[at] = { ...merged[at], value: p.value };
      else merged.push(kv(p.key, p.value));
    }
    patch({ env: merged });
    setText('');
    setPaste(false);
  };

  return (
    <Section title={t('create.env')} note={t('create.env.note')}>
      {spec.env.length === 0 && !paste && <p className="dk-muted dk-cr-empty">{t('create.env.empty')}</p>}
      {spec.env.map((e, i) => {
        const secret = isSecretKey(e.key) && !shown.has(e.id);
        return (
          <div className="dk-cr-pr" key={e.id}>
            <Input mono compact value={e.key} placeholder="NAME" aria-label={t('create.env.key')} onChange={(ev) => patch({ env: replaceAt(spec.env, i, { key: ev.target.value }) })} fieldClassName="dk-cr-fld" spellCheck={false} autoCapitalize="off" />
            <Input
              mono
              compact
              type={secret ? 'password' : 'text'}
              value={e.value}
              placeholder={t('create.env.value')}
              aria-label={t('create.env.value')}
              onChange={(ev) => patch({ env: replaceAt(spec.env, i, { value: ev.target.value }) })}
              fieldClassName="dk-cr-fld"
              autoComplete="off"
              spellCheck={false}
              end={isSecretKey(e.key) ? <IconButton icon={shown.has(e.id) ? 'eyeoff' : 'eye'} label={shown.has(e.id) ? t('create.env.hide') : t('create.env.show')} size="sm" onClick={() => setShown((s) => { const n = new Set(s); if (n.has(e.id)) n.delete(e.id); else n.add(e.id); return n; })} /> : undefined}
            />
            <IconButton icon="trash" label={t('common.remove')} onClick={() => patch({ env: removeAt(spec.env, i) })} />
          </div>
        );
      })}
      {showErrors && problems.env && <Hint tone="err" icon="alert">{t(problems.env)}</Hint>}
      {paste && (
        <div className="dk-cr-paste">
          <Textarea mono rows={7} value={text} onChange={(e) => setText(e.target.value)} placeholder={'DB_HOST=db\nDB_PASSWORD=secret\n# comments are ignored'} label={t('create.env.pasteLabel')} spellCheck={false} />
          <div className="dk-cr-row">
            <Button size="sm" variant="primary" disabled={!parseDotenv(text).length} onClick={apply}>{t('create.env.apply')}</Button>
            <Button size="sm" onClick={() => { setPaste(false); setText(''); }}>{t('common.cancel')}</Button>
          </div>
        </div>
      )}
      <div className="dk-cr-row">
        <Button className="dk-cr-add" variant="ghost" size="sm" icon="plus" onClick={() => patch({ env: [...spec.env, kv()] })}>{t('create.env.add')}</Button>
        {!paste && <Button className="dk-cr-add" variant="ghost" size="sm" icon="upload" onClick={() => setPaste(true)}>{t('create.env.import')}</Button>}
      </div>
    </Section>
  );
}
