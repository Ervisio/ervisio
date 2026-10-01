// Docker plugin for LinuxAdmin. Plain ES module, no build step: it uses the SDK's React and ui kit.
export default function activate(sdk) {
  const React = sdk.react;
  const h = React.createElement;
  const { useState, useEffect, useCallback, useRef } = React;
  const { Page, Card, Table, Badge, Button, Tabs, Dialog, EmptyState, Skeleton, StatCard, toast } = sdk.ui;

  sdk.registerStrings({
    en: {
      title: 'Docker',
      subtitle: 'Containers and images on this machine',
      containers: 'Containers',
      images: 'Images',
      refresh: 'Refresh',
      name: 'Name',
      image: 'Image',
      state: 'State',
      status: 'Status',
      ports: 'Ports',
      actions: 'Actions',
      start: 'Start',
      stop: 'Stop',
      restart: 'Restart',
      logs: 'Logs',
      close: 'Close',
      repository: 'Repository',
      tag: 'Tag',
      size: 'Size',
      created: 'Created',
      none: 'No containers yet.',
      noneText: 'Containers you create with docker run or compose appear here.',
      noImages: 'No images.',
      failed: 'Docker did not answer',
      failedText: 'Check that Docker is installed and running, and that your account may use it.',
      done: '{name}: {action} done',
      actionFailed: 'Could not {action} {name}',
      running: 'Running',
      stopped: 'Stopped',
      total: '{count} in total',
      empty: 'Nothing was logged.',
      widgetRunning: 'running',
    },
    it: {
      title: 'Docker',
      subtitle: 'Container e immagini su questa macchina',
      containers: 'Container',
      images: 'Immagini',
      refresh: 'Aggiorna',
      name: 'Nome',
      image: 'Immagine',
      state: 'Stato',
      status: 'Dettagli',
      ports: 'Porte',
      actions: 'Azioni',
      start: 'Avvia',
      stop: 'Ferma',
      restart: 'Riavvia',
      logs: 'Log',
      close: 'Chiudi',
      repository: 'Repository',
      tag: 'Tag',
      size: 'Dimensione',
      created: 'Creata',
      none: 'Ancora nessun container.',
      noneText: 'I container creati con docker run o compose compaiono qui.',
      noImages: 'Nessuna immagine.',
      failed: 'Docker non risponde',
      failedText: 'Controlla che Docker sia installato e attivo e che il tuo account possa usarlo.',
      done: '{name}: {action} completato',
      actionFailed: 'Impossibile eseguire {action} su {name}',
      running: 'In esecuzione',
      stopped: 'Fermi',
      total: '{count} in totale',
      empty: 'Nessun log.',
      widgetRunning: 'attivi',
    },
  });

  const t = (k, v) => sdk.t(k, v);

  function parseLines(out) {
    return String(out || '')
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean)
      .map((l) => {
        try {
          return JSON.parse(l);
        } catch {
          return null;
        }
      })
      .filter(Boolean);
  }

  async function execOk(command, args) {
    const r = await sdk.api.exec(command, args || []);
    if (r.exitCode !== 0) throw new Error((r.stderr || '').trim() || 'exit ' + r.exitCode);
    return r;
  }

  const tone = (state) => (state === 'running' ? 'ok' : state === 'paused' || state === 'restarting' ? 'warn' : state === 'exited' || state === 'dead' ? 'err' : 'neutral');

  /** Loads the container list; reloads on demand. */
  function useContainers() {
    const [rows, setRows] = useState(null);
    const [error, setError] = useState(null);
    const load = useCallback(async () => {
      try {
        const r = await execOk('ps');
        setRows(parseLines(r.stdout));
        setError(null);
      } catch (e) {
        setError(e && e.message ? e.message : String(e));
        setRows((x) => x || []);
      }
    }, []);
    useEffect(() => {
      load();
      const id = setInterval(load, 10000);
      return () => clearInterval(id);
    }, [load]);
    return { rows, error, load };
  }

  function LogsDialog({ name, onClose }) {
    const [text, setText] = useState(null);
    useEffect(() => {
      let live = true;
      sdk.api
        .exec('logs', [name])
        .then((r) => live && setText((r.stdout + r.stderr).trim() || t('empty')))
        .catch((e) => live && setText(e && e.message ? e.message : String(e)));
      return () => {
        live = false;
      };
    }, [name]);
    return h(
      Dialog,
      { open: true, onClose, title: t('logs') + ': ' + name, size: 'lg', footer: h(Button, { variant: 'primary', onClick: onClose }, t('close')) },
      text === null
        ? h(Skeleton, { lines: 6 })
        : h('pre', { style: { margin: 0, maxHeight: '50vh', overflow: 'auto', fontFamily: 'var(--mono, monospace)', fontSize: 12.5, whiteSpace: 'pre-wrap', wordBreak: 'break-all' } }, text),
    );
  }

  function ContainersTable({ rows, load }) {
    const [busy, setBusy] = useState('');
    const [logs, setLogs] = useState(null);
    const act = async (name, action) => {
      setBusy(name + action);
      try {
        await execOk(action, [name]);
        toast.ok(t('done', { name, action: t(action).toLowerCase() }));
      } catch (e) {
        toast.err(t('actionFailed', { name, action: t(action).toLowerCase() }), e && e.message ? e.message : String(e));
      } finally {
        setBusy('');
        load();
      }
    };
    const columns = [
      { key: 'name', header: t('name'), sortable: true, value: (r) => r.Names, render: (r) => h('b', null, r.Names) },
      { key: 'image', header: t('image'), sortable: true, value: (r) => r.Image, mono: true },
      { key: 'state', header: t('state'), sortable: true, value: (r) => r.State, render: (r) => h(Badge, { tone: tone(r.State) }, r.State) },
      { key: 'status', header: t('status'), value: (r) => r.Status },
      { key: 'ports', header: t('ports'), value: (r) => r.Ports || '', mono: true },
      {
        key: 'actions',
        header: t('actions'),
        render: (r) => {
          const running = r.State === 'running';
          return h(
            'div',
            { style: { display: 'flex', gap: 6, flexWrap: 'wrap' } },
            running
              ? h(Button, { size: 'sm', icon: 'stop', loading: busy === r.Names + 'stop', onClick: (e) => { e.stopPropagation(); act(r.Names, 'stop'); } }, t('stop'))
              : h(Button, { size: 'sm', icon: 'play', loading: busy === r.Names + 'start', onClick: (e) => { e.stopPropagation(); act(r.Names, 'start'); } }, t('start')),
            h(Button, { size: 'sm', icon: 'refresh', loading: busy === r.Names + 'restart', onClick: (e) => { e.stopPropagation(); act(r.Names, 'restart'); } }, t('restart')),
            h(Button, { size: 'sm', variant: 'ghost', icon: 'logs', onClick: (e) => { e.stopPropagation(); setLogs(r.Names); } }, t('logs')),
          );
        },
      },
    ];
    return h(
      React.Fragment,
      null,
      h(Table, { columns, rows, rowKey: (r) => r.ID, empty: h(EmptyState, { icon: 'server', hue: 'file', title: t('none'), text: t('noneText') }) }),
      logs && h(LogsDialog, { name: logs, onClose: () => setLogs(null) }),
    );
  }

  function ImagesTable() {
    const [rows, setRows] = useState(null);
    useEffect(() => {
      let live = true;
      execOk('images')
        .then((r) => live && setRows(parseLines(r.stdout)))
        .catch(() => live && setRows([]));
      return () => {
        live = false;
      };
    }, []);
    if (!rows) return h(Skeleton, { lines: 4 });
    const columns = [
      { key: 'repo', header: t('repository'), sortable: true, value: (r) => r.Repository, render: (r) => h('b', null, r.Repository) },
      { key: 'tag', header: t('tag'), value: (r) => r.Tag, mono: true },
      { key: 'size', header: t('size'), value: (r) => r.Size },
      { key: 'created', header: t('created'), value: (r) => r.CreatedSince },
    ];
    return h(Table, { columns, rows, rowKey: (r) => r.ID + r.Tag + r.Repository, empty: h(EmptyState, { icon: 'server', hue: 'file', title: t('noImages') }) });
  }

  function DockerPage() {
    const { rows, error, load } = useContainers();
    const [tab, setTab] = useState('containers');
    const running = (rows || []).filter((r) => r.State === 'running').length;
    return h(
      Page,
      {
        title: t('title'),
        subtitle: t('subtitle'),
        hue: 'file',
        actions: h(Button, { icon: 'refresh', onClick: load }, t('refresh')),
      },
      h(
        'div',
        { style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 12, marginBottom: 16 } },
        h(StatCard, { hue: 'file', icon: 'play', label: t('running'), value: rows ? running : '–' }),
        h(StatCard, { hue: 'svc', icon: 'stop', label: t('stopped'), value: rows ? rows.length - running : '–' }),
      ),
      h(Tabs, { variant: 'pill', hue: 'file', value: tab, onChange: setTab, items: [{ id: 'containers', label: t('containers'), count: rows ? rows.length : undefined }, { id: 'images', label: t('images') }] }),
      h(
        'div',
        { style: { marginTop: 12 } },
        error
          ? h(EmptyState, { icon: 'alert', hue: 'svc', title: t('failed'), text: error || t('failedText') })
          : rows === null
            ? h(Skeleton, { lines: 5 })
            : tab === 'containers'
              ? h(ContainersTable, { rows, load })
              : h(ImagesTable),
      ),
    );
  }

  function ContainersWidget() {
    const { rows, error } = useContainers();
    const running = rows ? rows.filter((r) => r.State === 'running').length : null;
    return h(StatCard, {
      hue: 'file',
      icon: 'server',
      label: t('containers'),
      value: error ? '–' : running === null ? '…' : running,
      unit: error ? '' : ' ' + t('widgetRunning'),
      sub: error ? t('failed') : rows ? t('total', { count: rows.length }) : '',
    });
  }

  sdk.registerPage('docker', DockerPage);
  sdk.registerWidget({ id: 'containers', title: t('containers'), icon: 'server', cols: 3, render: ContainersWidget });
  sdk.registerSnippet({ name: 'docker ps', command: 'docker ps' });
}
