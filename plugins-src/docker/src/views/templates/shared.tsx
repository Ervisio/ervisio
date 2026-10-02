import { COMPOSE_PROJECT, type Container } from '../../api/types';
import { slug, type Template } from '../../api/templates';

/** Letter tile in the hue of the app. */
export function Tile({ name, hue, size = 'md' }: { name: string; hue: string; size?: 'sm' | 'md' | 'lg' }) {
  return (
    <span className={`dk-tp-tile dk-tp-tile--${size} hue-${hue}`} aria-hidden="true">
      {name.trim().charAt(0).toUpperCase() || '?'}
    </span>
  );
}

const repoOf = (image: string): string => image.replace(/@.*$/, '').replace(/:[^/:]*$/, '');

/** Whether a container (or compose project) that looks like this template already exists. */
export function isInstalled(t: Template, list: Container[] | undefined): boolean {
  if (!list) return false;
  if (t.type === 'container' && t.container) {
    const repo = repoOf(t.container.image).replace(/^(docker\.io|index\.docker\.io)\//, '').replace(/^library\//, '');
    return list.some((c) => repoOf(c.Image).replace(/^(docker\.io|index\.docker\.io)\//, '').replace(/^library\//, '') === repo);
  }
  const names = new Set([slug(t.name), t.id.split(':')[1]]);
  return list.some((c) => names.has(c.Labels?.[COMPOSE_PROJECT] ?? '\0'));
}
