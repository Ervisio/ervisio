/** Offline, deterministic interpretation of the supplied concept's Bloom wallpaper. */
export function Wallpaper({ kind = 'bloom' }: { kind?: string }) {
  return (
    <div className={`eos-wallpaper eos-wallpaper--${kind}`} aria-hidden="true">
      <div className="eos-bloom">
        <i />
        <i />
        <i />
        <i />
        <i />
        <i />
        <i />
        <i />
      </div>
      <div className="eos-wallpaper-light" />
    </div>
  );
}
