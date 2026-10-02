/**
 * All CSS of the plugin as one <style>, injected once. Every file in this folder ending in .css is included
 * (alphabetical order), so a view adds its styles by dropping `myview.css` here. xterm's CSS is included too.
 * Use theme variables only; prefix class names with `dk-`.
 */
import xtermCss from '@xterm/xterm/css/xterm.css?inline';

const files = import.meta.glob<string>('./*.css', { query: '?inline', import: 'default', eager: true });

export function injectStyles(): void {
  if (document.getElementById('dk-styles')) return;
  const el = document.createElement('style');
  el.id = 'dk-styles';
  el.textContent = Object.keys(files).sort().map((k) => files[k]).join('\n') + '\n' + xtermCss;
  document.head.appendChild(el);
}
