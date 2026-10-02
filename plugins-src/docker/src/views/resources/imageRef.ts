/** Image reference helpers shared by the pull form, the update check and the images table. */

/** "nginx" -> { name: "nginx", tag: "latest" }, "a/b:1" -> { name: "a/b", tag: "1" }, "a/b@sha256:.." -> digest. */
export function splitRef(ref: string): { name: string; tag: string; digest?: string } {
  const r = ref.trim();
  const at = r.indexOf('@');
  if (at >= 0) return { name: r.slice(0, at).replace(/:[^/:]*$/, ''), tag: '', digest: r.slice(at + 1) };
  const i = r.lastIndexOf(':');
  if (i > r.lastIndexOf('/')) return { name: r.slice(0, i), tag: r.slice(i + 1) };
  return { name: r, tag: 'latest' };
}

/** A reference Docker will accept: lower case path parts, optional host:port, optional :tag or @digest. */
export const isValidRef = (ref: string): boolean =>
  /^[a-zA-Z0-9][a-zA-Z0-9._-]*(:[0-9]+)?(\/[a-z0-9][a-z0-9._-]*)*(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(@sha256:[a-f0-9]{64})?$/.test(ref.trim());

/** Path of an Engine call for an image reference: slashes stay, other characters are escaped per segment. */
export const refPath = (ref: string): string => ref.split('/').map(encodeURIComponent).join('/');

/** "repo:tag" entries of an image summary, without the dangling placeholder. */
export const realTags = (tags: string[] | null | undefined): string[] => (tags ?? []).filter((x) => x && x !== '<none>:<none>');
