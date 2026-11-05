import { useCallback, useEffect, useRef, useState } from 'react';
import { errorMessage, stream, type StreamHandle } from '../../api';
import { useT } from '../../i18n';
import { useWindowActivity } from '../../lib/windowActivity';
import { Button, Icon, Input } from '../../ui';
import { remoteLocation, remoteTarget } from '../files/remoteLocation';
import type { ProjectRoot } from './ProjectTree';

interface Match {
  path: string;
  line: number;
  column: number;
  text: string;
}
interface Message {
  matches?: Match[];
  files?: number;
  skipped?: number;
  count?: number;
  done?: boolean;
  truncated?: boolean;
  reason?: string;
}
export function ProjectSearch({
  root,
  onOpen,
}: {
  root: ProjectRoot;
  onOpen(path: string, line: number, column: number): void;
}) {
  const t = useT('editor');
  const active = useWindowActivity();
  const [query, setQuery] = useState('');
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [includeGenerated, setIncludeGenerated] = useState(false);
  const [matches, setMatches] = useState<Match[]>([]);
  const [progress, setProgress] = useState<Message>({});
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');
  const handle = useRef<StreamHandle | null>(null);
  const generation = useRef(0);
  const stop = useCallback(() => {
    generation.current++;
    handle.current?.close();
    handle.current = null;
    setRunning(false);
  }, []);
  useEffect(
    () => () => {
      generation.current++;
      handle.current?.close();
    },
    [],
  );
  useEffect(() => {
    if (!active) stop();
  }, [active, stop]);
  const search = () => {
    stop();
    if (!query.trim() || !active) return;
    const current = generation.current;
    const target = remoteTarget(root.path);
    setMatches([]);
    setProgress({});
    setError('');
    setRunning(true);
    handle.current = stream<Message>(
      target ? 'remote.searchText' : 'files.searchText',
      {
        root: target?.path ?? root.path,
        ...(target ? { conn: target.conn } : {}),
        query,
        caseSensitive,
        includeGenerated,
        maxResults: 300,
      },
      {
        admin: !target && root.admin,
        onData: (message) => {
          if (current !== generation.current) return;
          if (message.matches?.length)
            setMatches((matches) => [...matches, ...message.matches!].slice(0, 300));
          setProgress(message);
          if (message.done) {
            setRunning(false);
            handle.current?.close();
            handle.current = null;
          }
        },
        onError: (error) => {
          if (current === generation.current) {
            setError(errorMessage(error));
            setRunning(false);
          }
        },
        onEnd: () => {
          if (current === generation.current) setRunning(false);
        },
      },
    );
  };
  const remote = remoteTarget(root.path);
  return (
    <div className="editor-project-search">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          search();
        }}
      >
        <Input
          aria-label={t('project.query')}
          placeholder={t('project.query')}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          autoFocus
          maxLength={256}
        />
        <div className="editor-search-options">
          <label>
            <input
              type="checkbox"
              checked={caseSensitive}
              onChange={(event) => setCaseSensitive(event.target.checked)}
            />
            {t('project.caseSensitive')}
          </label>
          <label>
            <input
              type="checkbox"
              checked={includeGenerated}
              onChange={(event) => setIncludeGenerated(event.target.checked)}
            />
            {t('project.includeGenerated')}
          </label>
        </div>
        <div className="editor-search-controls">
          <Button type="submit" size="sm" icon="search" disabled={!query.trim()}>
            {t('project.search')}
          </Button>
          {running && (
            <Button type="button" size="sm" onClick={stop}>
              {t('project.stop')}
            </Button>
          )}
        </div>
      </form>
      {error && (
        <p role="alert" className="editor-tree-notice">
          {error}
        </p>
      )}
      <p className="editor-search-summary" role="status">
        {running ? t('project.searching') : t('project.results', { count: matches.length })}
        {progress.files !== undefined && ` · ${t('project.scanned', { count: progress.files })}`}
      </p>
      {!!progress.skipped && (
        <p className="editor-tree-notice">{t('project.skipped', { count: progress.skipped })}</p>
      )}
      {progress.truncated && <p className="editor-tree-notice">{t('project.searchLimit')}</p>}
      <div className="editor-search-results">
        {matches.map((match, index) => (
          <button
            type="button"
            key={`${match.path}:${match.line}:${index}`}
            onClick={() =>
              onOpen(
                remote ? remoteLocation(remote.conn, match.path) : match.path,
                match.line,
                match.column,
              )
            }
            title={`${match.path}:${match.line}`}
          >
            <b>
              <Icon name="file" size={13} />
              {match.path.split(/[\\/]/).pop()}
              <small>
                {match.line}:{match.column}
              </small>
            </b>
            <span>{match.text}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
