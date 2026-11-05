// VITE_MOCK fixture only. This never invokes a filesystem, service or real reset.
import { ApiError } from '../../api/types';

export function mockResetCall(method: string, params: Record<string, any>, admin?: boolean) {
  if (!admin) throw new ApiError('needs_admin', 'Administrator rights needed');
  if (method === 'maintenance.reset.preview') {
    localStorage.setItem(
      'ervisio.mock.resetPreserveAccess',
      JSON.stringify(!!params.preserveAccess),
    );
    return {
      token: 'fixture-plan-only',
      expiresAt: Date.now() + 300000,
      paths: [{ path: '/fixture/ervisio/profiles', files: 4, bytes: 2048 }],
      preserved: ['/fixture/ervisio/plugins', '/fixture/ervisio/audit', '/fixture/recovery.swap'],
      files: 4,
      bytes: 2048,
      backupRoot: '/fixture/protected-backups',
      nextURL: params.preserveAccess ? location.origin : 'https://console.example:9090',
    };
  }
  if (method === 'maintenance.reset.apply') {
    if (params.token !== 'fixture-plan-only' || params.confirm !== 'RESET ERVISIO')
      throw new ApiError('invalid', 'Confirm the reviewed reset plan.');
    localStorage.setItem('ervisio.mock.resetApplied', 'fixture-only');
    return {
      id: '1'.repeat(32),
      state: 'running',
      phase: 'backup',
      expectedBackupPath: '/fixture/protected-backups/factory-reset-fixture.tar.gz',
      statusToken: '2'.repeat(64),
      nextURL: location.origin,
    };
  }
  if (method === 'maintenance.reset.status')
    return {
      id: '1'.repeat(32),
      state: 'done',
      phase: 'complete',
      backup: '/fixture/protected-backups/factory-reset-fixture.tar.gz',
      restartRequired: false,
    };
  if (method === 'maintenance.reset.restart') return { scheduled: true };
  throw new ApiError('not_found', 'Unknown reset fixture method');
}
