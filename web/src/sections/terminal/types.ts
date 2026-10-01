export type Kind = 'local' | 'root' | 'ssh';

export interface SessionInfo {
  id: string;
  name: string;
  cwd: string;
  createdAt: number;
  attached: boolean;
  kind: Kind;
  title: string;
  /** Lives in the root bridge: attach and manage it with admin:true. */
  admin: boolean;
}

export interface Host {
  id: string;
  name: string;
  host: string;
  user?: string;
  port?: number;
}

export interface Snippet {
  id: string;
  title: string;
  command: string;
}

export interface HistoryItem {
  cmd: string;
  at: number;
}

export interface TermHandle {
  /** Insert text at the prompt (bracketed paste when the app asks for it), without running it. */
  paste(text: string): void;
  /** Insert text and press Enter. */
  run(cmd: string): void;
  /** Send raw key bytes. */
  sendKeys(seq: string): void;
  /** Arrow keys honour the application cursor mode. */
  arrow(dir: 'up' | 'down' | 'left' | 'right'): void;
  focus(): void;
  copySelection(): void;
}

export const newId = () => Math.random().toString(36).slice(2, 10);
