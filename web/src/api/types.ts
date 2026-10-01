export type ErrorCode =
  | 'needs_admin'
  | 'forbidden'
  | 'not_found'
  | 'invalid'
  | 'conflict'
  | 'unavailable'
  | 'internal'
  | 'unauthenticated'
  | 'network'
  | 'cancelled';

export class ApiError extends Error {
  code: ErrorCode | string;
  data?: unknown;
  status?: number;
  constructor(code: ErrorCode | string, message: string, data?: unknown, status?: number) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.data = data;
    this.status = status;
  }
}

export interface CallOptions {
  /** Run on the root bridge (needs unlock). */
  admin?: boolean;
  signal?: AbortSignal;
  /** Do not open the unlock dialog on needs_admin; just throw. */
  noUnlock?: boolean;
}

export interface Session {
  user: string;
  /** GECOS full name, may be empty. */
  name?: string;
  uid: number;
  home?: string;
  groups?: string[];
  isRoot?: boolean;
  /** Admin rights usable right now (root, or unlocked and not expired). */
  isAdmin: boolean;
  /** Hint: root or member of wheel/sudo/admin. sudo decides on unlock. */
  canSudo: boolean;
  /** Epoch milliseconds (normalised from whatever the server sends). */
  unlockedUntil?: number;
}

export interface DistroInfo {
  id: string;
  name: string;
  color?: string;
  logo?: string;
  /** Present only when the icon file was found: /api/public/logo */
  logoUrl?: string;
}

export interface PublicHost {
  hostname: string;
  ip?: string;
  distro: DistroInfo;
}

export interface LoginResult {
  user: string;
  isAdmin: boolean;
  isRoot: boolean;
}

export interface StreamHandlers<T = any> {
  /** Text payloads arrive as parsed JSON, b64 payloads as Uint8Array. */
  onData?: (data: T) => void;
  onEnd?: () => void;
  onError?: (err: ApiError) => void;
  /** Run the stream on the root bridge. */
  admin?: boolean;
  /** Re-send the open frame if the socket reconnects (good for log follows). Default false. */
  reopen?: boolean;
}

export interface StreamHandle {
  /** Send an input frame; `data` is forwarded as-is (any JSON) to the module's stream. Use toBase64() for bytes. */
  send(data: unknown): void;
  close(): void;
}
