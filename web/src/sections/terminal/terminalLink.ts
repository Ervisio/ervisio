const COMMAND_MAX = 1000;

/** Routes can select a working directory; it is passed as PTY metadata, never shell input. */
export function terminalLink(params: URLSearchParams) {
  const cwd = params.get('cwd') || undefined;
  const command = params.get('cmd');
  if (!cwd && !command) return null;
  return {
    cwd,
    command: command ? command.replace(/[\r\n]+/g, ' ').slice(0, COMMAND_MAX) : undefined,
    admin: params.get('admin') === '1',
  };
}
