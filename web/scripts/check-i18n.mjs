// Checks that every namespace has the same keys in every language. Usage: npm run i18n:check
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../src/i18n/', import.meta.url).pathname;
const langs = readdirSync(root, { withFileTypes: true }).filter((d) => d.isDirectory()).map((d) => d.name);
const flat = (o, p = '') => Object.entries(o).flatMap(([k, v]) => (v && typeof v === 'object' ? flat(v, `${p}${k}.`) : [`${p}${k}`]));
const spaces = new Set(langs.flatMap((l) => readdirSync(join(root, l)).filter((f) => f.endsWith('.json')).map((f) => f.slice(0, -5))));
let bad = 0;
for (const ns of spaces) {
  const sets = Object.fromEntries(langs.map((l) => {
    try { return [l, new Set(flat(JSON.parse(readFileSync(join(root, l, `${ns}.json`), 'utf8'))))]; }
    catch { return [l, null]; }
  }));
  for (const l of langs) {
    if (!sets[l]) { console.log(`${l}/${ns}.json missing or invalid`); bad++; continue; }
    for (const other of langs) {
      if (!sets[other]) continue;
      for (const k of sets[other]) if (!sets[l].has(k)) { console.log(`${l}/${ns}: missing "${k}"`); bad++; }
    }
  }
}
console.log(bad ? `${bad} problem(s)` : 'i18n ok');
process.exit(bad ? 1 : 0);
