import type { ActivitySearch } from '../api/activation';
/** The closed UI address space. Old product routes are deliberately absent. */
export const paths = {
  workspaces: '/workspaces',
  workspaceOverview: '/workspaces/$slug/overview',
  workspaceServices: '/workspaces/$slug/services',
  workspaceModels: '/workspaces/$slug/services/models',
  workspaceModelRoute: '/workspaces/$slug/services/models/$route',
  workspaceMCP: '/workspaces/$slug/services/mcp',
  workspaceSearch: '/workspaces/$slug/services/search',
  workspaceAccess: '/workspaces/$slug/access',
  workspaceLimits: '/workspaces/$slug/limits',
  workspaceConnect: '/workspaces/$slug/connect',
  connections: '/connections',
  connection: '/connections/$id',
  principals: '/access',
  principal: '/access/$principalId',
  activity: '/activity',
  exchange: '/activity/$operationId',
  history: '/settings/history',
  network: '/settings/network',
  administration: '/settings/administration',
} as const;
export const pageRoutes = Object.values(paths);

export const globalPages = ['Workspaces', 'Connections', 'Access', 'Activity', 'Settings'] as const;
export const workspaceSections = ['Overview', 'Services', 'Access', 'Limits', 'Connect'] as const;

export function pageContext(path: string) {
  const segments = path.split('/').filter(Boolean);
  const global = globalPages.find(page => page.toLowerCase() === segments[0]);
  const workspace = segments[0] === 'workspaces' && segments.length > 2 ? segments[1] : undefined;
  const section = workspace ? segments[2] : undefined;
  const title = workspace ? decodeURIComponent(workspace) :
    segments[0] === 'settings' ? ({ history: 'History & exports', network: 'Network', administration: 'Administration' }[segments[1]] ?? 'Settings') :
    segments.length > 1 ? decodeURIComponent(segments[1]) : global ?? 'Page not found';
  return { global, workspace, section, title };
}


/** URL state owns history scope and continuation. Changing evidence kind
 * drops filters that its owning Ledger query cannot apply. */
export function activitySearch(raw: Record<string, unknown>): ActivitySearch {
  const kind = raw.kind === 'exchange' || raw.kind === 'mcp_operation' ? raw.kind : 'all';
  const text = (key: string) => typeof raw[key] === 'string' && raw[key] !== '' ? raw[key] as string : undefined;
  return { workspace: text('workspace'), kind, cursor: text('cursor'),
    from: kind === 'exchange' ? text('from') : undefined,
    to: kind === 'exchange' ? text('to') : undefined,
    principal: kind === 'exchange' ? text('principal') : undefined,
    result: kind === 'exchange' ? text('result') : undefined };
}
