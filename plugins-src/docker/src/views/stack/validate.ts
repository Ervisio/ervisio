/**
 * Live checks of a compose file and a .env file. These are quick structure checks, not a replacement for
 * `docker compose config` (which the deploy runs anyway): YAML syntax, services present, image or build per service,
 * ports format, unknown depends_on and volume references.
 */
import { isMap, isScalar, isSeq, LineCounter, parseDocument, type Node, type Pair } from 'yaml';

export interface Issue {
  /** 1-based line. */
  line: number;
  level: 'error' | 'warn';
  /** i18n key (stacks.v.*) and its variables. */
  key: string;
  vars?: Record<string, string | number>;
}

export interface ParsedService {
  name: string;
  image: string;
  line: number;
}

export interface Validation {
  issues: Issue[];
  services: ParsedService[];
  errors: number;
  warnings: number;
}

const PORT = /^(?:(?:\[[0-9a-fA-F:.]+\]|[0-9.]+):)?(?:\d{1,5}(?:-\d{1,5})?:)?\d{1,5}(?:-\d{1,5})?(?:\/(?:tcp|udp|sctp))?$/;

const hasVar = (s: string) => s.includes('$');

export function validateCompose(text: string, envText = ''): Validation {
  const issues: Issue[] = [];
  const services: ParsedService[] = [];
  const done = (): Validation => {
    issues.sort((a, b) => a.line - b.line);
    return { issues, services, errors: issues.filter((i) => i.level === 'error').length, warnings: issues.filter((i) => i.level === 'warn').length };
  };
  if (!text.trim()) {
    issues.push({ line: 1, level: 'error', key: 'stacks.v.empty' });
    return done();
  }
  const lc = new LineCounter();
  const doc = parseDocument(text, { lineCounter: lc, prettyErrors: false, uniqueKeys: true });
  const lineOf = (n: unknown): number => {
    const r = (n as Node | null | undefined)?.range;
    return r ? lc.linePos(r[0]).line : 1;
  };
  for (const e of doc.errors) {
    issues.push({ line: e.linePos?.[0]?.line ?? 1, level: 'error', key: 'stacks.v.syntax', vars: { msg: e.message.split('\n')[0].replace(/ at line \d+, column \d+.*$/, '') } });
  }
  if (doc.errors.length) return done();

  const root = doc.contents;
  if (!isMap(root)) {
    issues.push({ line: 1, level: 'error', key: 'stacks.v.notMap' });
    return done();
  }
  const pairOf = (m: { items: Pair<unknown, unknown>[] }, key: string): Pair | undefined => m.items.find((p) => isScalar(p.key) && p.key.value === key) as Pair | undefined;

  const ver = pairOf(root, 'version');
  if (ver) issues.push({ line: lineOf(ver.key), level: 'warn', key: 'stacks.v.version' });

  const svcPair = pairOf(root, 'services');
  if (!svcPair || !isMap(svcPair.value) || svcPair.value.items.length === 0) {
    issues.push({ line: svcPair ? lineOf(svcPair.key) : 1, level: 'error', key: 'stacks.v.noServices' });
    return done();
  }
  const svcMap = svcPair.value;
  const names = new Set(svcMap.items.map((p) => String((p.key as { value?: unknown })?.value)));
  const declared = (key: string): Set<string> => {
    const p = pairOf(root, key);
    return new Set(p && isMap(p.value) ? p.value.items.map((q) => String((q.key as { value?: unknown })?.value)) : []);
  };
  const volumes = declared('volumes');
  const networks = declared('networks');
  const usedVars = new Set<string>();
  const collectVars = (s: string) => {
    for (const m of s.matchAll(/\$\{([A-Za-z_][A-Za-z0-9_]*)([^}]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)/g)) {
      const name = m[1] ?? m[3];
      const rest = m[2] ?? '';
      if (!/^:?[-?+]/.test(rest)) usedVars.add(name);
    }
  };

  for (const p of svcMap.items) {
    const name = String((p.key as { value?: unknown })?.value);
    const kl = lineOf(p.key);
    if (!isMap(p.value)) {
      issues.push({ line: kl, level: 'error', key: 'stacks.v.svcNotMap', vars: { name } });
      continue;
    }
    const sm = p.value;
    const image = pairOf(sm, 'image');
    const build = pairOf(sm, 'build');
    const imageVal = image && isScalar(image.value) ? String(image.value.value ?? '') : '';
    services.push({ name, image: imageVal, line: kl });
    if (!image && !build && !pairOf(sm, 'extends')) issues.push({ line: kl, level: 'error', key: 'stacks.v.noImage', vars: { name } });
    if (image && !imageVal.trim()) issues.push({ line: lineOf(image.key), level: 'error', key: 'stacks.v.emptyImage', vars: { name } });

    const ports = pairOf(sm, 'ports');
    if (ports && isSeq(ports.value)) {
      for (const item of ports.value.items) {
        if (isScalar(item)) {
          const v = String(item.value ?? '').trim();
          if (hasVar(v)) continue;
          const nums = (v.split('/')[0].match(/\d+/g) ?? []).map(Number);
          if (!PORT.test(v)) issues.push({ line: lineOf(item), level: 'error', key: 'stacks.v.port', vars: { value: v } });
          else if (nums.some((n) => n < 1 || n > 65535)) issues.push({ line: lineOf(item), level: 'error', key: 'stacks.v.portRange', vars: { value: v } });
        } else if (isMap(item)) {
          if (!pairOf(item, 'target')) issues.push({ line: lineOf(item), level: 'error', key: 'stacks.v.portTarget' });
        }
      }
    } else if (ports && !isSeq(ports.value)) {
      issues.push({ line: lineOf(ports.key), level: 'error', key: 'stacks.v.portsList', vars: { name } });
    }

    const dep = pairOf(sm, 'depends_on');
    if (dep) {
      const refs: { n: string; node: unknown }[] = [];
      if (isSeq(dep.value)) dep.value.items.forEach((i) => isScalar(i) && refs.push({ n: String(i.value), node: i }));
      else if (isMap(dep.value)) dep.value.items.forEach((i) => refs.push({ n: String((i.key as { value?: unknown })?.value), node: i.key }));
      for (const r of refs) {
        if (r.n === name) issues.push({ line: lineOf(r.node), level: 'error', key: 'stacks.v.depSelf', vars: { name } });
        else if (!names.has(r.n)) issues.push({ line: lineOf(r.node), level: 'error', key: 'stacks.v.depUnknown', vars: { name, dep: r.n } });
      }
    }

    const vols = pairOf(sm, 'volumes');
    if (vols && isSeq(vols.value)) {
      for (const item of vols.value.items) {
        let src = '';
        if (isScalar(item)) {
          const parts = String(item.value ?? '').split(':');
          if (parts.length >= 2) src = parts[0];
        } else if (isMap(item)) {
          const ty = pairOf(item, 'type');
          const s = pairOf(item, 'source');
          if (ty && isScalar(ty.value) && ty.value.value === 'volume' && s && isScalar(s.value)) src = String(s.value.value);
        }
        if (src && !hasVar(src) && !/^[./~]/.test(src) && !volumes.has(src)) issues.push({ line: lineOf(item), level: 'error', key: 'stacks.v.volUnknown', vars: { name, vol: src } });
      }
    }
    const nets = pairOf(sm, 'networks');
    if (nets) {
      const list: { n: string; node: unknown }[] = [];
      if (isSeq(nets.value)) nets.value.items.forEach((i) => isScalar(i) && list.push({ n: String(i.value), node: i }));
      else if (isMap(nets.value)) nets.value.items.forEach((i) => list.push({ n: String((i.key as { value?: unknown })?.value), node: i.key }));
      for (const r of list) if (!['default', 'host', 'none', 'bridge'].includes(r.n) && !networks.has(r.n)) issues.push({ line: lineOf(r.node), level: 'error', key: 'stacks.v.netUnknown', vars: { name, net: r.n } });
    }
  }

  // ${VARS} without a default that .env does not define.
  const env = parseEnv(envText);
  for (const m of text.matchAll(/^.*$/gm)) {
    if (/^\s*#/.test(m[0])) continue;
    usedVars.clear();
    collectVars(m[0]);
    for (const v of usedVars) {
      if (!(v in env)) {
        const idx = m.index ?? 0;
        issues.push({ line: lc.linePos(idx).line, level: 'warn', key: 'stacks.v.varUnset', vars: { name: v } });
      }
    }
  }
  return done();
}

export function parseEnv(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const l of text.split('\n')) {
    const m = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_.-]*)\s*=(.*)$/.exec(l);
    if (m) out[m[1]] = m[2];
  }
  return out;
}

export function validateEnv(text: string): Issue[] {
  const out: Issue[] = [];
  const seen = new Map<string, number>();
  text.split('\n').forEach((l, i) => {
    if (!l.trim() || /^\s*#/.test(l)) return;
    const m = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_.-]*)\s*=/.exec(l);
    if (!m) out.push({ line: i + 1, level: 'error', key: 'stacks.v.envLine' });
    else if (seen.has(m[1])) out.push({ line: i + 1, level: 'warn', key: 'stacks.v.envDup', vars: { name: m[1], line: seen.get(m[1])! } });
    else seen.set(m[1], i + 1);
  });
  return out;
}
