import type { DiskUsage } from './types';

export interface DiskPart {
  key: 'images' | 'containers' | 'volumes' | 'cache';
  hue: 'sw' | 'file' | 'term' | 'log';
  total: number;
  /** Bytes a clean-up could free. */
  reclaimable: number;
}

/** Splits /system/df into the four parts of the shared "Disk used by Docker" bar. */
export function summarizeDf(df: DiskUsage): DiskPart[] {
  const imgs = df.Images ?? [];
  const imgTotal = df.LayersSize || imgs.reduce((a, i) => a + i.Size, 0);
  const imgFree = Math.min(imgTotal, imgs.filter((i) => i.Containers === 0).reduce((a, i) => a + Math.max(0, i.Size - Math.max(0, i.SharedSize)), 0));
  const cts = df.Containers ?? [];
  const ctTotal = cts.reduce((a, c) => a + (c.SizeRw ?? 0), 0);
  const ctFree = cts.filter((c) => c.State !== 'running' && c.State !== 'restarting' && c.State !== 'paused').reduce((a, c) => a + (c.SizeRw ?? 0), 0);
  const vols = df.Volumes ?? [];
  const size = (v: { UsageData?: { Size: number } | null }) => Math.max(0, v.UsageData?.Size ?? 0);
  const volTotal = vols.reduce((a, v) => a + size(v), 0);
  const volFree = vols.filter((v) => (v.UsageData?.RefCount ?? 1) === 0).reduce((a, v) => a + size(v), 0);
  const bc = df.BuildCache ?? [];
  const bcTotal = bc.reduce((a, b) => a + b.Size, 0);
  const bcFree = bc.filter((b) => !b.InUse).reduce((a, b) => a + b.Size, 0);
  return [
    { key: 'images', hue: 'sw', total: imgTotal, reclaimable: imgFree },
    { key: 'containers', hue: 'file', total: ctTotal, reclaimable: ctFree },
    { key: 'volumes', hue: 'term', total: volTotal, reclaimable: volFree },
    { key: 'cache', hue: 'log', total: bcTotal, reclaimable: bcFree },
  ];
}
