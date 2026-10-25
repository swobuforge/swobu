import { createRootRoute, createRoute, createRouter, redirect } from '@tanstack/react-router';
import { Shell, RouteContent, NotFound } from './shell';
import { paths } from './routes';
import { activitySearch } from './routes';

const root = createRootRoute({ component: Shell, notFoundComponent: NotFound });
const index = createRoute({ getParentRoute: () => root, path: '/', beforeLoad: () => { throw redirect({ to: '/workspaces' }); } });
const workspaces = createRoute({ getParentRoute: () => root, path: paths.workspaces, component: RouteContent });
const workspaceOverview = createRoute({ getParentRoute: () => root, path: paths.workspaceOverview, component: RouteContent });
const workspaceServices = createRoute({ getParentRoute: () => root, path: paths.workspaceServices, component: RouteContent });
const workspaceModels = createRoute({ getParentRoute: () => root, path: paths.workspaceModels, component: RouteContent });
const workspaceModelRoute = createRoute({ getParentRoute: () => root, path: paths.workspaceModelRoute, component: RouteContent });
const workspaceMCP = createRoute({ getParentRoute: () => root, path: paths.workspaceMCP, component: RouteContent });
const workspaceSearch = createRoute({ getParentRoute: () => root, path: paths.workspaceSearch, component: RouteContent });
const workspaceAccess = createRoute({ getParentRoute: () => root, path: paths.workspaceAccess, component: RouteContent });
const workspaceLimits = createRoute({ getParentRoute: () => root, path: paths.workspaceLimits, component: RouteContent });
const workspaceConnect = createRoute({ getParentRoute: () => root, path: paths.workspaceConnect, component: RouteContent });
const connections = createRoute({ getParentRoute: () => root, path: paths.connections, component: RouteContent });
const connection = createRoute({ getParentRoute: () => root, path: paths.connection, component: RouteContent });
const principals = createRoute({ getParentRoute: () => root, path: paths.principals, component: RouteContent });
const principal = createRoute({ getParentRoute: () => root, path: paths.principal, component: RouteContent });
const activity = createRoute({ getParentRoute: () => root, path: paths.activity, component: RouteContent,
  validateSearch: activitySearch,
});
const exchange = createRoute({ getParentRoute: () => root, path: paths.exchange, component: RouteContent, validateSearch: activitySearch });
const history = createRoute({ getParentRoute: () => root, path: paths.history, component: RouteContent, validateSearch: raw => activitySearch({ ...raw, kind: raw.kind === 'mcp_operation' ? 'mcp_operation' : 'exchange' }) });
const network = createRoute({ getParentRoute: () => root, path: paths.network, component: RouteContent });
const administration = createRoute({ getParentRoute: () => root, path: paths.administration, component: RouteContent });
export const router = createRouter({ routeTree: root.addChildren([index, workspaces, workspaceOverview, workspaceServices, workspaceModels, workspaceModelRoute, workspaceMCP, workspaceSearch, workspaceAccess, workspaceLimits, workspaceConnect, connections, connection, principals, principal, activity, exchange, history, network, administration]), defaultPreload: 'intent' });

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}
