export interface NetworkPath {
  host: string;
  port: number;
  share: string;
  path: string;
}

/** Parse Windows UNC or smb:// paths without ever accepting credentials in an address. */
export function parseNetworkPath(input: string): NetworkPath | null {
  const value = input.trim();
  if (!value || /[\0\r\n]/.test(value)) return null;
  // URL normalisation removes dot segments; reject them before it can change the share.
  try {
    if (value.split(/[\\/]/).some((part) => ['.', '..'].includes(decodeURIComponent(part))))
      return null;
  } catch {
    return null;
  }
  let host: string,
    port = 445,
    parts: string[];
  if (/^smb:\/\//i.test(value)) {
    let url: URL;
    try {
      url = new URL(value);
    } catch {
      return null;
    }
    if (url.username || url.password || url.search || url.hash || !url.hostname) return null;
    host = url.hostname.replace(/^\[|\]$/g, '');
    if (url.port) port = Number(url.port);
    try {
      parts = url.pathname.split('/').filter(Boolean).map(decodeURIComponent);
    } catch {
      return null;
    }
  } else if (value.startsWith('\\\\') || value.startsWith('//')) {
    parts = value
      .replace(/^[\\/]+/, '')
      .split(/[\\/]/)
      .filter(Boolean);
    host = parts.shift() ?? '';
  } else return null;
  if (
    !host ||
    /[\s\\/@?#]/.test(host) ||
    !Number.isInteger(port) ||
    port < 1 ||
    port > 65535 ||
    !parts.length
  )
    return null;
  if (
    parts.some(
      (part) =>
        part === '.' ||
        part === '..' ||
        /[\\/]/.test(part) ||
        Array.from(part).some((char) => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127),
    )
  )
    return null;
  const share = parts.shift()!;
  if (share.length > 80 || /[:*?"<>|]/.test(share)) return null;
  return { host, port, share, path: '/' + parts.join('/') };
}

export const networkAddress = (host: string, share: string, path = '/', port = 445) =>
  port === 445
    ? `\\\\${host}\\${share}${path === '/' ? '' : path.replaceAll('/', '\\')}`
    : `smb://${host.includes(':') ? `[${host}]` : host}:${port}/${share}${path === '/' ? '' : path}`;
