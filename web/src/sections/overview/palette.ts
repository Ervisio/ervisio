import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { usePrefs } from '../../api';
import { useT } from '../../i18n';
import type { PaletteAction } from '../registry';
import { requestConfirm, runAction, showOutput } from './actions';
import { layoutActions, normalizeLayout } from './model';

/** Always-on palette actions of the Overview: Personalize and every custom action. */
export default function useOverviewPalette(): PaletteAction[] {
  const t = useT('overview');
  const nav = useNavigate();
  const { prefs } = usePrefs();
  const dash = prefs.dashboard;
  return useMemo(() => {
    const list: PaletteAction[] = [
      { id: 'overview.personalize', title: t('palette.personalize'), hint: t('title'), icon: 'edit', hue: 'ov', keywords: [t('palette.kwDashboard'), t('palette.kwWidgets'), t('palette.kwLayout')], run: () => nav('/?personalize=1') },
    ];
    const seen = new Set<string>();
    for (const a of layoutActions(normalizeLayout(dash))) {
      if (seen.has(a.id)) continue;
      seen.add(a.id);
      list.push({
        id: `overview.run.${a.id}`,
        title: t('palette.run', { name: a.label }),
        hint: a.argv.join(' '),
        icon: a.icon,
        hue: a.hue,
        keywords: [a.label, ...a.argv],
        run: () => {
          if (a.confirm) {
            requestConfirm(a);
            nav('/');
            return;
          }
          void runAction(a, t, (v) => {
            showOutput(v);
            nav('/');
          });
        },
      });
    }
    return list;
  }, [t, dash, nav]);
}
