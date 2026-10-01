export interface LoginInfo {
  at: number;
  from?: string;
  tty?: string;
}

export interface UserInfo {
  name: string;
  uid: number;
  gid: number;
  primaryGroup: string;
  fullName: string;
  home: string;
  shell: string;
  groups: string[];
  isAdmin: boolean;
  locked: boolean | null;
  passwordState: 'ok' | 'locked' | 'disabled' | 'empty' | 'unknown';
  passwordChanged: number | null;
  mustChange: boolean;
  expired: boolean;
  noLoginShell: boolean;
  lastLogin: LoginInfo | null;
  neverLoggedIn: boolean;
}

export interface UserList {
  people: UserInfo[];
  system: UserInfo[];
  uidMin: number;
  shadowReadable: boolean;
  lastLoginKnown: boolean;
  adminGroup: string;
  shells: string[];
  self: string;
}

export interface GroupInfo {
  name: string;
  gid: number;
  members: string[];
  primaryMembers: string[];
  system: boolean;
  description: string;
}

export interface SshKey {
  type: string;
  comment: string;
  fingerprint: string;
  options: string;
  key: string;
  line: number;
}

export interface SessionInfo {
  id: string;
  user: string;
  uid: number;
  type: string;
  class: string;
  tty: string;
  remote: boolean;
  remoteHost: string;
  service: string;
  seat: string;
  state: string;
  since: number;
}
