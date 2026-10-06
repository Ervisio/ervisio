export interface Command {
  name: string;
  description?: string;
  argv: string[];
  args?: { pattern: string; allowDash?: boolean; maxLen?: number }[];
  admin: boolean;
  adminUnlessGroup?: string;
  timeoutSec?: number;
  /** Runs only in a terminal (sdk.api.pty). */
  pty?: boolean;
  /** Systems this entry is for; missing = all the plugin's. */
  platforms?: string[];
}
/** An HTTP API on a unix socket (capabilities.http, SDK v3). */
export interface HttpApi {
  name: string;
  socket: string;
  admin: boolean;
  adminUnlessGroup?: string;
  headers: string[];
  rules: { methods: string[]; path: string }[];
  maxBody?: number;
  maxUpload?: number;
  timeoutSec?: number;
  /** Systems this entry is for; on Windows `socket` may be a named pipe (\\.\pipe\name). */
  platforms?: string[];
}
/** A capabilities.files entry: a path, or an object with admin / create (SDK v3). */
export type Folder = string | { path: string; admin?: boolean; adminUnlessGroup?: string; create?: boolean; platforms?: string[] };
export interface Capabilities {
  commands: Command[];
  /** Missing from older daemons and catalogs. */
  http?: HttpApi[];
  files: { read: Folder[]; write: Folder[] };
  sockets: string[];
  network: string[];
  /** Background jobs (core 0.5): named steps over the plugin's own commands and HTTP APIs. */
  jobs?: { name: string; steps?: { command?: string; http?: { api: string } }[] }[];
  /** May send notifications to the administrator's channels (core 0.5). */
  notify?: boolean;
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
  /** Logo file name in the plugin folder (logo.svg / logo.png). */
  logo?: string;
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
  /** Systems the plugin works on ('linux', 'windows'). */
  platforms?: string[];
  /** The plugin cannot run here (another system, or it needs a newer Ervisio): the daemon's message says why. */
  incompatible?: string;
  /** Unsigned dev-folder plugin running only because developer mode is on. */
  devUnsigned?: boolean;
  error?: string;
}
export interface CatalogEntry {
  id: string;
  name: string;
  version: string;
  author: string;
  description: string;
  icon: string;
  /** Logo as a data: URL from the signed catalog. */
  logo?: string;
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
  /** Systems the plugin works on ('linux', 'windows'); the daemon fills in ['linux'] when the entry does not say. */
  platforms?: string[];
  /** Set by the daemon when the plugin cannot run here: another system, or an Ervisio older than it needs (minCore / requires). */
  incompatible?: string;
}
export interface CatalogCategory {
  id: string;
  name: string;
  icon: string;
  color: string;
}
/** A plugin that used to ship with Ervisio and now comes from the marketplace (plugins.catalog "moved"). */
export interface MovedNotice {
  id: string;
  name: string;
  version: string;
}
export interface CatalogView {
  categories: CatalogCategory[];
  plugins: CatalogEntry[];
  warning?: string;
  /** Missing from older daemons. */
  moved?: MovedNotice[];
}
export type TabId = 'installed' | 'updates' | 'browse' | 'security' | 'developer';
