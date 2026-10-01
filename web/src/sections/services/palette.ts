import { useMemo } from 'react';
import { call, ApiError } from '../../api';
import { useT } from '../../i18n';
import { toast } from '../../ui';
import type { PaletteAction } from '../registry';
import { useBackgroundPoll, useShared } from './store';

/** Command palette: "Restart <unit>" for every running service. */
export default function usePalette(): PaletteAction[] {
  const t = useT('services');
  useBackgroundPoll(true);
  const { running } = useShared();
  return useMemo(
    () =>
      running.map((name) => ({
        id: `services.restart.${name}`,
        title: t('palette.restart', { name: name.replace(/\.service$/, '') }),
        hint: t('title'),
        icon: 'refresh' as const,
        hue: 'svc',
        keywords: [name, t('restart')],
        run: () => {
          const short = name.replace(/\.service$/, '');
          call('services.action', { name, action: 'restart' }).then(
            () => toast.ok(t('toast.restarted', { name: short })),
            (e) => toast.err(t('toast.failed', { name: short }), e instanceof ApiError ? e.message : String(e)),
          );
        },
      })),
    [running, t],
  );
}
