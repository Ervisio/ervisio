export type Dict = Record<string, string>;

export function flatten(o: Record<string, unknown>, prefix = '', out: Dict = {}): Dict {
  for (const [k, v] of Object.entries(o)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object') flatten(v as Record<string, unknown>, key, out);
    else out[key] = String(v);
  }
  return out;
}
