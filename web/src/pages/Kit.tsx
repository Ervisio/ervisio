import { useState } from 'react';
import {
  AreaChart, Badge, Button, Card, Checkbox, Chip, ConfirmDialog, Dialog, DropdownMenu, EmptyState, Field, Icon, IconButton, Input, Kbd, Menu, Page,
  Panel, Progress, Radio, Segmented, Select, Sheet, Skeleton, Sparkline, StatCard, Switch, Table, Tabs, Textarea, toast, Tooltip, UnlockDialog, useContextMenu,
  type MenuItem,
} from '../ui';

/** Dev-only visual check of every component (route /__kit). Strings here are intentionally not translated. */
const rows = [
  { id: 'nginx', name: 'nginx', state: 'Running', mem: 14.2, port: 80 },
  { id: 'docker', name: 'docker', state: 'Failed', mem: 182, port: 2375 },
  { id: 'sshd', name: 'sshd', state: 'Running', mem: 6.1, port: 22 },
  { id: 'cups', name: 'cups', state: 'Stopped', mem: 0, port: 631 },
];
const series = (seed: number) => Array.from({ length: 40 }, (_, i) => 40 + 25 * Math.sin(i / 4 + seed) + 8 * Math.cos(i / 1.7 + seed));

const items: MenuItem[] = [
  { type: 'heading', label: 'nginx.service' },
  { id: 'r', label: 'Restart', icon: 'refresh', kbd: 'R', onSelect: () => toast.ok('nginx restarted') },
  { id: 'l', label: 'Reload', icon: 'play', onSelect: () => toast.info('Reloaded') },
  { type: 'separator' },
  { id: 'd', label: 'Disable and stop', icon: 'trash', danger: true, onSelect: () => toast.err("Couldn't stop docker", 'Job for docker.service canceled.') },
];

export default function Kit() {
  const [tab, setTab] = useState('a');
  const [tab2, setTab2] = useState('x');
  const [sw, setSw] = useState(true);
  const [cb, setCb] = useState(true);
  const [rd, setRd] = useState('a');
  const [seg, setSeg] = useState('table');
  const [sel, setSel] = useState('b');
  const [dlg, setDlg] = useState<null | 'dlg' | 'confirm' | 'unlock' | 'sheet'>(null);
  const [panel, setPanel] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(new Set(['sshd']));
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);
  const ctx = useContextMenu(() => items);
  const close = () => setDlg(null);

  return (
    <Page title="Component kit" subtitle="Every component in the current theme. Dev only.">
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(340px,1fr))', gap: 18 }}>
        <Card title="Buttons">
          <div className="row" style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
            <Button variant="primary" icon="download">Update all</Button>
            <Button>Cancel</Button>
            <Button variant="ghost">Skip</Button>
            <Button variant="danger" icon="trash">Delete</Button>
            <Button variant="danger-solid">Delete user</Button>
            <IconButton icon="refresh" label="Refresh" variant="secondary" />
            <IconButton icon="more" label="More" />
            <Button variant="primary" loading>Restarting…</Button>
            <Button disabled>Disabled</Button>
            <Button size="sm">Small</Button>
          </div>
        </Card>
        <Card title="Inputs">
          <div style={{ display: 'grid', gap: 14 }}>
            <Input label="Username" icon="users" defaultValue="mario" />
            <Input label="Port" defaultValue="80" error="Port 80 is used by nginx. Pick another, e.g. 9090." />
            <Input label="Search" icon="search" kbd="Ctrl K" placeholder="Find" hint="Hint text" />
            <Select label="Shell" value={sel} onChange={setSel} options={[{ value: 'a', label: '/usr/bin/bash' }, { value: 'b', label: '/usr/bin/zsh' }]} />
            <Textarea label="Notes" mono defaultValue={'key = "value"'} />
            <Field label="Custom field"><div className="ui-in">Anything</div></Field>
          </div>
        </Card>
        <Card title="Choices">
          <div style={{ display: 'grid', gap: 14 }}>
            <div style={{ display: 'flex', gap: 20, flexWrap: 'wrap' }}>
              <Switch checked={sw} onChange={setSw} label="Start at boot" />
              <Switch checked={false} onChange={() => undefined} label="Notify me" />
              <Switch checked disabled onChange={() => undefined} label="Disabled" />
            </div>
            <div style={{ display: 'flex', gap: 20, flexWrap: 'wrap' }}>
              <Checkbox checked={cb} onChange={setCb} label="wheel" />
              <Checkbox checked={false} onChange={() => undefined} label="video" />
              <Checkbox checked={false} indeterminate onChange={() => undefined} label="some" />
            </div>
            <div style={{ display: 'flex', gap: 20 }}>
              <Radio checked={rd === 'a'} onChange={() => setRd('a')} label="Self-signed" name="r" />
              <Radio checked={rd === 'b'} onChange={() => setRd('b')} label="Let's Encrypt" name="r" />
            </div>
            <Segmented value={seg} onChange={setSeg} options={[{ value: 'table', label: 'Table' }, { value: 'cards', label: 'Cards' }]} aria-label="View" />
            <Segmented value={seg} onChange={setSeg} options={[{ value: 'table', icon: 'list', title: 'List' }, { value: 'cards', icon: 'grid', title: 'Grid' }]} aria-label="Layout" />
          </div>
        </Card>
        <Card title="Status">
          <div style={{ display: 'grid', gap: 14 }}>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <Badge tone="ok">Running</Badge><Badge tone="err">Failed</Badge><Badge tone="warn">Restarting</Badge><Badge tone="info">Update ready</Badge><Badge>Stopped</Badge>
            </div>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <Chip pressed count={142}>All</Chip><Chip count={1}>Failed</Chip><Chip icon="server">Web</Chip><Kbd>Ctrl K</Kbd>
            </div>
            <Progress value={62} /><Progress /><Progress value={90} tone="err" />
            <Skeleton lines={3} />
          </div>
        </Card>
        <Card title="Tabs">
          <div style={{ display: 'grid', gap: 18 }}>
            <Tabs variant="pill" value={tab} onChange={setTab} items={[{ id: 'a', label: 'Updates', count: 23 }, { id: 'b', label: 'Find software' }, { id: 'c', label: 'Installed' }]} aria-label="Pill tabs" />
            <Tabs variant="underline" value={tab2} onChange={setTab2} items={[{ id: 'x', label: 'Info', icon: 'info' }, { id: 'y', label: 'Logs', icon: 'logs' }, { id: 'z', label: 'Unit file' }]} aria-label="Underline tabs" />
          </div>
        </Card>
        <Card title="Toasts and menus">
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <Button onClick={() => toast.ok('nginx restarted')}>Success</Button>
            <Button onClick={() => toast.err("Couldn't stop docker", 'Job for docker.service canceled.')}>Error</Button>
            <Button onClick={() => toast.undo('Theme saved', 'Undo', () => toast.info('Undone'))}>Undo</Button>
            <Button onClick={() => { const id = toast.show({ title: 'Updating 18 packages', tone: 'run', progress: 20 }); let p = 20; const iv = setInterval(() => { p += 20; toast.update(id, { progress: p }); if (p >= 100) { clearInterval(iv); toast.update(id, { title: 'Updated 18 packages', tone: 'ok', progress: undefined, duration: 3000 }); } }, 700); }}>Progress</Button>
            <DropdownMenu items={items} trigger={(p) => <Button {...p} icon="more">Dropdown</Button>} />
            <Button onClick={(e) => setMenuAt({ x: e.clientX, y: e.clientY })}>Menu here</Button>
            <span {...ctx.bind} style={{ padding: '8px 12px', borderRadius: 12, background: 'var(--sunk)' }}>Right-click me</span>
            <Tooltip label="Restart service"><Button iconOnly icon="refresh" aria-label="Restart" /></Tooltip>
          </div>
          {ctx.menu}
          {menuAt && <Menu items={items} anchor={menuAt} onClose={() => setMenuAt(null)} />}
        </Card>
        <Card title="Overlays">
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <Button onClick={() => setDlg('dlg')}>Dialog</Button>
            <Button onClick={() => setDlg('confirm')}>Type to confirm</Button>
            <Button onClick={() => setDlg('unlock')}>Administrator rights</Button>
            <Button onClick={() => setDlg('sheet')}>Sheet</Button>
            <Button onClick={() => setPanel(true)}>Side panel</Button>
          </div>
        </Card>
        <Card title="Empty state">
          <EmptyState icon="logs" title="No log files watched yet" text="Add a file like /srv/app/storage/logs/app.log to follow it here." action={<Button variant="primary" icon="plus">Watch a file</Button>} />
        </Card>
        <Card title="Stat cards">
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <StatCard hue="ov" icon="cpu" label="CPU" value="23" unit="%" percent={23} sub="16 cores" />
            <StatCard hue="term" icon="mem" label="Memory" value="9.2" unit="GB" percent={29} sub="of 32 GB" />
            <StatCard hue="log" icon="disk" label="Disk" value="412" unit="GB" spark={series(2)} />
            <StatCard hue="svc" icon="alert" label="Failed" value="1" sub="service" />
          </div>
        </Card>
        <Card title="Charts">
          <AreaChart series={[{ values: series(0), color: 'var(--h-ov)', label: 'CPU' }, { values: series(2), color: 'var(--h-file)', label: 'Network' }]} max={100} />
          <div style={{ marginTop: 14 }}><Sparkline values={series(1)} color="var(--h-term)" height={40} /></div>
        </Card>
        <div style={{ gridColumn: '1 / -1' }}>
          <Card title="Table">
            <Table
              caption="Services"
              rowKey={(r) => r.id}
              rows={rows}
              selectable
              selected={picked}
              onSelectedChange={setPicked}
              columns={[
                { key: 'name', header: 'Name', sortable: true, value: (r) => r.name, render: (r) => <b>{r.name}</b> },
                { key: 'state', header: 'State', sortable: true, value: (r) => r.state, render: (r) => <Badge tone={r.state === 'Running' ? 'ok' : r.state === 'Failed' ? 'err' : 'neutral'}>{r.state}</Badge> },
                { key: 'mem', header: 'Memory', sortable: true, align: 'right', value: (r) => r.mem, render: (r) => `${r.mem} MB` },
                { key: 'port', header: 'Port', mono: true, value: (r) => r.port },
              ]}
            />
          </Card>
        </div>
      </div>

      <Dialog open={dlg === 'dlg'} onClose={close} title="Plain dialog" description="Focus is trapped here. Press Escape to close." icon="info" footer={<Button variant="primary" onClick={close}>OK</Button>} />
      <ConfirmDialog open={dlg === 'confirm'} onClose={close} onConfirm={() => toast.ok('mario deleted')} title="Delete user mario?" description={<>The account and <code>/home/mario</code> (4.2 GB) will be removed. This can&apos;t be undone.</>} confirmLabel="Delete user" confirmText="mario">
        <Checkbox checked={false} onChange={() => undefined} label="Keep the home folder" />
      </ConfirmDialog>
      <UnlockDialog open={dlg === 'unlock'} user="fonlogen" reason="Restarting docker" onCancel={close} onSubmit={async (pw) => { if (pw === 'wrong') throw new Error('Wrong password. Try again.'); close(); }} />
      <Sheet open={dlg === 'sheet'} onClose={close} title="Bottom sheet"><p>Sheets are the phone version of side panels.</p><Button onClick={close}>Close</Button></Sheet>
      <Panel
        open={panel}
        onClose={() => setPanel(false)}
        title="nginx"
        subtitle="Running for 6 min"
        icon="services"
        hue="svc"
        tabs={<Tabs variant="underline" value={tab2} onChange={setTab2} items={[{ id: 'x', label: 'Info' }, { id: 'y', label: 'Logs' }, { id: 'z', label: 'Unit' }]} />}
        footer={<><Button icon="refresh">Restart</Button><Button variant="danger" icon="power">Stop</Button></>}
      >
        <Skeleton lines={4} />
        <Progress value={40} />
        <Icon name="info" />
      </Panel>
    </Page>
  );
}
