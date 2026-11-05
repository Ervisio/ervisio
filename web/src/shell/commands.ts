/** Local UI intents only. No shell, filesystem or bridge RPC runs here. */
export interface ShellCommandResult {
  handled: boolean;
  message?: string;
}

export interface ShellApp {
  id: string;
  title: string;
  to: string;
  aliases?: string[];
}

export type LocalShellIntent =
  | { kind: 'open'; app: ShellApp }
  | { kind: 'theme'; value: 'dark' | 'light' | 'toggle' }
  | { kind: 'mode'; value: 'classic' | 'ervisios' }
  | { kind: 'window'; action: 'show-desktop' | 'close-window' | 'minimize-window' };

const normalize = (text: string) =>
  text.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase().trim().replace(/[.!?]+$/, '').replace(/\s+/g, ' ');

/** Whole-message matches prevent a mixed request from silently losing its second action. */
export function parseShellCommand(text: string, apps: ShellApp[]): LocalShellIntent | null {
  if (typeof text !== 'string' || text.length > 500) return null;
  const value = normalize(text);
  if (/^(?:(?:passa|metti|imposta|attiva)(?: al| il)? |switch to |set |enable )?(?:tema scuro|modalita scura|dark theme|dark mode)$/.test(value))
    return { kind: 'theme', value: 'dark' };
  if (/^(?:(?:passa|metti|imposta|attiva)(?: al| il)? |switch to |set |enable )?(?:tema chiaro|modalita chiara|light theme|light mode)$/.test(value))
    return { kind: 'theme', value: 'light' };
  if (/^(?:cambia (?:il )?tema|inverti (?:il )?tema|toggle (?:the )?theme|change (?:the )?theme)$/.test(value))
    return { kind: 'theme', value: 'toggle' };
  if (/^(?:mostra (?:il )?desktop|vai al desktop|show (?:the )?desktop)$/.test(value))
    return { kind: 'window', action: 'show-desktop' };
  if (/^(?:chiudi (?:la )?finestra(?: attiva)?|close (?:the )?(?:active )?window)$/.test(value))
    return { kind: 'window', action: 'close-window' };
  if (/^(?:minimizza (?:la )?finestra(?: attiva)?|riduci (?:la )?finestra a icona|minimize (?:the )?(?:active )?window)$/.test(value))
    return { kind: 'window', action: 'minimize-window' };
  if (/^(?:passa a|attiva|usa|switch to|enable|use) ervisios$/.test(value))
    return { kind: 'mode', value: 'ervisios' };
  if (/^(?:passa a|attiva|usa|switch to|enable|use) (?:ervis[i]?o classico|interfaccia classica|classic(?: interface)?)$/.test(value))
    return { kind: 'mode', value: 'classic' };
  const match = value.match(/^(?:apri|avvia|mostra|open|launch)(?: (?:il|lo|la|le|gli|l'|the))?\s+(.+)$/);
  if (!match) return null;
  const target = match[1];
  const candidates = apps.filter((app) => [app.id, app.title, ...(app.aliases ?? [])].some((name) => normalize(name) === target));
  return candidates.length === 1 ? { kind: 'open', app: candidates[0] } : null;
}

type Handler = (text: string) => ShellCommandResult | Promise<ShellCommandResult>;
let handler: Handler | undefined;

/** One authenticated application shell owns the handler; stale cleanups cannot replace a newer one. */
export function setShellCommandHandler(next: Handler): () => void {
  handler = next;
  return () => { if (handler === next) handler = undefined; };
}

export async function runShellCommand(text: string): Promise<ShellCommandResult> {
  if (!handler || typeof text !== 'string' || text.length > 500) return { handled: false };
  return handler(text);
}
