export interface Command {
  name: string;
  description?: string;
  argv: string[];
  args?: { pattern: string; allowDash?: boolean; maxLen?: number }[];
  admin: boolean;
  adminUnlessGroup?: string;
  timeoutSec?: number;
}
export interface Capabilities {
  commands: Command[];
  files: { read: string[]; write: string[] };
  sockets: string[];
  network: string[];
}
export interface Contribution {
  id: string;
  title: string;
  icon?: string;
}
export interface Contributes {
  pages: Contribution[];
  widgets: Contribution[];
  snippets: { name: string; command: string }[];
}
export interface UpdateInfo {
  version: string;
  notes?: string;
  newPermissions: boolean;
  source?: string;
  sha256?: string;
}
export interface PluginInfo {
  id: string;
  name: string;
  version: string;
  author: string;
  description: string;
  icon: string;
  color: string;
  entry: string;
  enabled: boolean;
  signed: boolean;
  verified: boolean;
  signatureError?: string;
  capabilities: Capabilities;
  contributes: Contributes;
  visibleTo: { groups: string[] };
  location: 'system' | 'installed' | 'dev';
  removable: boolean;
  unloadable?: boolean;
  dir?: string;
  updateAvailable?: UpdateInfo;
  blocked?: boolean;
  error?: string;
}
export interface CatalogEntry {
  id: string;
  name: string;
  version: string;
  author: string;
  description: string;
  icon: string;
  color: string;
  category: string;
  verified: boolean;
  installs: number;
  featured?: boolean;
  notes?: string;
  source?: string;
  sha256?: string;
  capabilities: Capabilities;
  contributes: Contributes;
  visibleTo: { groups: string[] };
  installed: boolean;
  installedVersion?: string;
}
export interface CatalogCategory {
  id: string;
  name: string;
  icon: string;
  color: string;
}
export interface CatalogView {
  categories: CatalogCategory[];
  plugins: CatalogEntry[];
  warning?: string;
}
export type TabId = 'installed' | 'updates' | 'browse' | 'security' | 'developer';
