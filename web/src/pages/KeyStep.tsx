import { useRef, useState, type DragEvent, type KeyboardEvent } from 'react';
import { describeKey, needsPassphrase, publicInfo, SshKeyError, type SshKeyErrorCode } from '../auth/sshkey';
import { useT } from '../i18n';
import { Icon, IconButton, Input } from '../ui';

/** A private key held in memory only (never written to storage). */
export interface LoadedKey {
  text: string;
  /** File name, or '' for a pasted key. */
  name: string;
  needsPass: boolean;
  /** Only known for keys without a passphrase (an encrypted key cannot be read before decrypting). */
  type?: string;
  fingerprint?: string;
  comment?: string;
}

const MAX_BYTES = 256 * 1024;

const TYPE_NAMES: Record<string, string> = {
  'ssh-ed25519': 'ED25519',
  'ecdsa-sha2-nistp256': 'ECDSA P-256',
  'ecdsa-sha2-nistp384': 'ECDSA P-384',
  'ecdsa-sha2-nistp521': 'ECDSA P-521',
  'ssh-rsa': 'RSA',
};

/** Reads and checks a key text. Resolves to the key, or rejects with an SshKeyError (not a key, public key, ...). */
async function inspect(text: string, name: string): Promise<LoadedKey> {
  const t = text.trim();
  if (needsPassphrase(t)) {
    // Still check the format (public key, PuTTY...) before asking for a passphrase.
    try {
      await describeKey(t, '');
    } catch (e) {
      if (!(e instanceof SshKeyError) || e.code !== 'passphrase_required') throw e;
    }
    const p = await publicInfo(t);
    return { text: t, name, needsPass: true, ...(p ? { type: `${TYPE_NAMES[p.type] ?? p.type}${p.type === 'ssh-rsa' ? ` ${p.bits}` : ''}`, fingerprint: p.fingerprint } : {}) };
  }
  const d = await describeKey(t, '');
  return { text: t, name, needsPass: false, type: `${TYPE_NAMES[d.type] ?? d.type}${d.type === 'ssh-rsa' ? ` ${d.bits}` : ''}`, fingerprint: d.fingerprint, comment: d.comment };
}

function readFile(f: File): Promise<string> {
  return new Promise((resolve, reject) => {
    if (f.size > MAX_BYTES) {
      reject(new SshKeyError('not_a_key', 'File too large'));
      return;
    }
    const r = new FileReader();
    r.onload = () => resolve(String(r.result ?? ''));
    r.onerror = () => reject(new SshKeyError('not_a_key', 'Could not read the file'));
    r.readAsText(f);
  });
}

export function KeyStep({ loaded, onLoaded, onClear, onError, passphrase, onPassphrase, passError, disabled }: {
  loaded: LoadedKey | null;
  onLoaded(k: LoadedKey): void;
  onClear(): void;
  onError(code: SshKeyErrorCode | null): void;
  passphrase: string;
  onPassphrase(v: string): void;
  passError?: boolean;
  disabled?: boolean;
}) {
  const t = useT('auth');
  const [tab, setTab] = useState<'file' | 'paste'>('file');
  const [over, setOver] = useState(false);
  const [paste, setPaste] = useState('');
  const [showPass, setShowPass] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const passRef = useRef<HTMLInputElement>(null);

  const accept = async (read: () => Promise<string>, name: string) => {
    onError(null);
    try {
      const k = await inspect(await read(), name);
      setPaste('');
      onLoaded(k);
      if (k.needsPass) window.setTimeout(() => passRef.current?.focus(), 0);
    } catch (e) {
      onError(e instanceof SshKeyError ? e.code : 'not_a_key');
    }
  };
  const pickFile = (f: File | undefined) => {
    if (f) void accept(() => readFile(f), f.name);
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setOver(false);
    pickFile(e.dataTransfer.files?.[0]);
  };
  const onDropKey = (e: KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      fileRef.current?.click();
    }
  };
  const clear = () => {
    onClear();
    onError(null);
    onPassphrase('');
    if (fileRef.current) fileRef.current.value = '';
  };

  if (loaded) {
    return (
      <>
        <div className="sk-card">
          <span className="sk-ic"><Icon name="key" /></span>
          <div className="sk-meta">
            <b>{loaded.name || t('key.pasted')}</b>
            <span className="mono">{loaded.type ? `${loaded.type} ${loaded.fingerprint}` : t('key.encrypted')}</span>
            {loaded.comment && <span className="sk-cm">{loaded.comment}</span>}
            {loaded.needsPass && <span className="sk-chip"><Icon name="lock" />{t('key.protected')}</span>}
          </div>
          <IconButton icon="x" label={t('key.change')} onClick={clear} disabled={disabled} />
        </div>
        {loaded.needsPass && (
          <Input
            ref={passRef}
            label={t('key.passphrase')}
            icon="lock"
            id="kp"
            name="key-passphrase"
            type={showPass ? 'text' : 'password'}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            placeholder={t('key.passphrasePlaceholder')}
            value={passphrase}
            onChange={(e) => onPassphrase(e.target.value)}
            error={passError ? true : undefined}
            disabled={disabled}
            end={<IconButton icon={showPass ? 'eyeoff' : 'eye'} label={showPass ? t('key.hidePassphrase') : t('key.showPassphrase')} onClick={() => setShowPass((s) => !s)} />}
          />
        )}
      </>
    );
  }

  return (
    <div>
      <input ref={fileRef} type="file" hidden onChange={(e) => pickFile(e.target.files?.[0])} />
      <div className="sk-tabs" role="tablist" aria-label={t('key.how')}>
        <button type="button" role="tab" aria-selected={tab === 'file'} className={tab === 'file' ? 'on' : ''} onClick={() => setTab('file')}>{t('key.tabFile')}</button>
        <button type="button" role="tab" aria-selected={tab === 'paste'} className={tab === 'paste' ? 'on' : ''} onClick={() => setTab('paste')}>{t('key.tabPaste')}</button>
      </div>
      {tab === 'file' ? (
        <div
          className={`sk-drop${over ? ' over' : ''}`}
          role="button"
          tabIndex={0}
          aria-label={t('key.dropAria')}
          onClick={() => fileRef.current?.click()}
          onKeyDown={onDropKey}
          onDragOver={(e) => {
            e.preventDefault();
            setOver(true);
          }}
          onDragLeave={() => setOver(false)}
          onDrop={onDrop}
        >
          <Icon name="upload" />
          <span><b>{t('key.dropTitle')}</b> {t('key.dropOr')} <em>{t('key.dropHint')}</em></span>
        </div>
      ) : (
        <textarea
          className="sk-paste"
          aria-label={t('key.pasteLabel')}
          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
          spellCheck={false}
          autoCapitalize="none"
          autoComplete="off"
          autoCorrect="off"
          value={paste}
          onChange={(e) => {
            const v = e.target.value;
            setPaste(v);
            if (/-----END [A-Z0-9 ]*PRIVATE KEY-----/.test(v) || /^ssh-(ed25519|rsa) /.test(v.trim()) || /^ecdsa-sha2-/.test(v.trim())) void accept(() => Promise.resolve(v), '');
            else onError(null);
          }}
        />
      )}
    </div>
  );
}
