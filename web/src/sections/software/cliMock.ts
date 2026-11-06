const key = 'ervisio.mock.cli';
const specs = [
  { id: 'node-lts', title: 'Node.js LTS', command: 'node', userScope: true },
  { id: 'claude-code', title: 'Claude Code', command: 'claude', userScope: true },
  { id: 'codex', title: 'Codex CLI', command: 'codex', userScope: true },
  { id: 'git', title: 'Git', command: 'git', userScope: false, package: 'git' },
  { id: 'restic', title: 'restic', command: 'restic', userScope: false, package: 'restic' },
];
export function mockSoftwareCLI() {
  const installed = JSON.parse(localStorage.getItem(key) ?? '[]') as string[];
  return specs.map((v) => ({
    ...v,
    docs: 'https://example.com/official-fixture',
    canInstall: true,
    installed: installed.includes(v.id),
    managed: installed.includes(v.id),
    version: installed.includes(v.id) ? '1.2.3' : '',
    terminalCommand: `/home/alice/.local/bin/${v.command}`,
  }));
}
export function mockCLIChange(op: string, id: string) {
  const installed = new Set(JSON.parse(localStorage.getItem(key) ?? '[]') as string[]);
  if (op === 'remove') installed.delete(id);
  else installed.add(id);
  localStorage.setItem(key, JSON.stringify([...installed]));
  const records = JSON.parse(localStorage.getItem('ervisio.mock.cli.calls') ?? '[]') as unknown[];
  localStorage.setItem(
    'ervisio.mock.cli.calls',
    JSON.stringify([...records, { op, id, admin: false }]),
  );
}
