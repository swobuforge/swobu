import type { components } from './generated/schema';
import { createControlClient } from './client';
import { ControlRequestError } from './transport';

// Domain wire types are generated from api/openapi.json. Authoring DTOs are
// intentionally distinct from redacted, non-editable read projections.
export type Workspace = components['schemas']['WorkspaceProjection'];
export type WorkspaceSummary = components['schemas']['WorkspaceSummary'];
export type WorkspaceService = components['schemas']['WorkspaceServiceState'];
export type Connection = components['schemas']['Connection'];
export type ConnectionKind = Connection['kind'];
export type ConnectionCreate = components['schemas']['ConnectionCreateDraft'];
export type ConnectionEdit = components['schemas']['ConnectionEditDraft'];
export type NodeOverview = components['schemas']['NodeOverview'];
export type ConnectionReferences = components['schemas']['ConnectionReferences'];
export type ConnectionCheck = components['schemas']['ConnectionCheck'];

const api = createControlClient();
function dataOrFail<T>(value: T | undefined): T {
  if (value === undefined) throw new Error('Node returned no resource.');
  return value;
}

export async function listWorkspaces(): Promise<WorkspaceSummary[]> {
  return dataOrFail((await api.GET('/api/v1/workspaces')).data);
}
export async function getWorkspace(slug: string): Promise<Workspace> {
  return dataOrFail((await api.GET('/api/v1/workspaces/{slug}', { params: { path: { slug } } })).data);
}
export async function createWorkspace(slug: string, displayName: string, intentKey: string): Promise<Workspace> {
  const result = await api.POST('/api/v1/workspaces', {
    params: { header: { 'Idempotency-Key': intentKey } },
    body: { slug, display_name: displayName },
  });
  return dataOrFail(result.data);
}
export async function renameWorkspace(workspace: Workspace, displayName: string): Promise<Workspace> {
  return dataOrFail((await api.PATCH('/api/v1/workspaces/{slug}', {
    params: { path: { slug: workspace.slug }, header: { 'If-Match': `"${workspace.management_etag}"` } },
    body: { display_name: displayName },
  })).data);
}
export async function listWorkspaceServices(slug: string): Promise<WorkspaceService[]> {
  return dataOrFail((await api.GET('/api/v1/workspaces/{slug}/services', { params: { path: { slug } } })).data);
}
export async function changeWorkspaceService(workspace: Workspace, service: WorkspaceService, enabled: boolean): Promise<Workspace> {
  const params = { path: { slug: workspace.slug, service: service.kind }, header: { 'If-Match': `"${workspace.management_etag}"` } };
  // The server owns the impact calculation. Only a displayed, exact review can
  // authorize expanding existing Workspace grants or relay reachability.
  const review = service.activation_review;
  if (enabled && review?.requires_confirmation && !review.digest) throw new Error('Reload the current Service impact before enabling.');
  return dataOrFail((enabled ? await api.PUT('/api/v1/workspaces/{slug}/services/{service}', {
    params, ...(review?.requires_confirmation ? { body: { review_digest: review.digest } } : {}),
  }) : await api.DELETE('/api/v1/workspaces/{slug}/services/{service}', { params })).data);
}
export async function listConnections(): Promise<Connection[]> {
  return dataOrFail((await api.GET('/api/v1/connections')).data);
}
export async function getConnection(id: string): Promise<Connection> {
  return dataOrFail((await api.GET('/api/v1/connections/{id}', { params: { path: { id } } })).data);
}
export async function getConnectionReferences(id: string): Promise<ConnectionReferences> {
  return dataOrFail((await api.GET('/api/v1/connections/{id}/references', { params: { path: { id } } })).data);
}
export async function createConnection(body: ConnectionCreate, intentKey: string): Promise<Connection> {
  const result = await api.POST('/api/v1/connections', { params: { header: { 'Idempotency-Key': intentKey } }, body });
  return dataOrFail(result.data).connection;
}
// A real, explicitly requested upstream probe. Checks do not mutate desired
// Node configuration and are tied to the reviewed Connection revision.
export async function checkConnection(connection: Connection): Promise<ConnectionCheck> {
  return dataOrFail((await api.POST('/api/v1/connections/{id}/checks', {
    params: { path: { id: connection.id }, header: { 'If-Match': `"${connection.revision}"` } },
  })).data);
}
export async function deleteConnection(connection: Connection): Promise<void> {
  await api.DELETE('/api/v1/connections/{id}', { params: { path: { id: connection.id }, header: { 'If-Match': `"${connection.revision}"` } } });
}
// Writes use the reviewed, redacted resource revision. Callers must supply a
// complete private authoring draft; the redacted read projection is never reused
// as input. Ambiguous PATCH responses require an explicit current-state read.
export async function editConnection(connection: Connection, draft: ConnectionEdit): Promise<Connection> {
  const result = await api.PATCH('/api/v1/connections/{id}', {
    params: { path: { id: connection.id }, header: { 'If-Match': `"${connection.revision}"` } }, body: draft,
  });
  return dataOrFail(result.data);
}

// Replacement material is write-only. Never pass it as a React Query mutation
// variable or retain it in a persisted/query result. Explicit retries must use
// the same key, revision and exact material to invoke durable server replay.
export async function replaceConnectionCredential(connection: Connection, secret: string, environment: string, intent: string): Promise<{ connection: Connection; replayed: boolean }> {
  const result = await api.POST('/api/v1/connections/{id}/credential-replacements', {
    params: { path: { id: connection.id }, header: {
      'If-Match': `"${connection.revision}"`, 'Idempotency-Key': intent,
    } }, body: { secret, same_identity: true, ...(environment ? { environment } : {}) },
  });
  return dataOrFail(result.data);
}
export async function getNodeOverview(): Promise<NodeOverview> {
  return dataOrFail((await api.GET('/api/v1/node')).data);
}

// Authoring projections use the generated Control contract.
export type RouteDraft = { tiers: Array<{ targets: Array<{
  id: string; model: string; protocol: string; connection_id: string;
}> }> };
export type ModelRoute = { name: string } & RouteDraft;
export type MCPMount = { alias: string; connection_id: string; builtin?: "swobu-admin"; selected_tools: string[] };
export type MCPTool = components['schemas']['ConnectionMCPCatalogTool'];
export type MCPMountReview = {
  workspace_etag: string; digest: string; connection_revision: string;
  access_revision: string; publication_revision: string;
};
export type MCPMountPreview = {
  workspace: string; alias: string; connection_id: string; builtin?: "swobu-admin"; endpoint: string;
  execution_identity: string; identity_verified: boolean; permission_summary: string;
  allowed_tools: string[]; potentially_effectful_tools: string[];
  affected_principals: Array<{ id: string; label: string }>;
  published: boolean;
  revision: string; access_revision: string; publication_revision: string;
  digest: string; connection_revision: string; approval_class: string; warning: string;
};
export type SearchRoute = { connection_id: string; fallback_connection_ids: string[] };

export type ModelMatch = components['schemas']['ModelRouteMatch'];

export async function setModelRouteOrder(workspace: Workspace, order: string[]): Promise<Workspace> {
  const result = await api.PATCH('/api/v1/workspaces/{slug}/model-routes', {
    params: { path: { slug: workspace.slug }, header: { 'If-Match': `"${workspace.management_etag}"` } },
    body: { order },
  });
  return dataOrFail(result.data as Workspace | undefined);
}
export async function previewModelMatch(slug: string, model: string): Promise<ModelMatch> {
  const result = await api.GET('/api/v1/workspaces/{slug}/model-match', {
    params: { path: { slug }, query: { model } },
  });
  return dataOrFail(result.data as ModelMatch | undefined);
}

export async function getModelRoute(slug: string, route: string): Promise<RouteDraft> {
  const result = await api.GET('/api/v1/workspaces/{slug}/model-routes/{route}', { params: { path: { slug, route } } });
  return dataOrFail(result.data as RouteDraft | undefined);
}
export async function putModelRoute(workspace: Workspace, route: string, draft: RouteDraft): Promise<Workspace> {
  const options = { params: { path: { slug: workspace.slug, route }, header: {
    'If-Match': `"${workspace.management_etag}"`,
  } }, body: draft };
  const result = await api.PUT('/api/v1/workspaces/{slug}/model-routes/{route}', options);
  return dataOrFail(result.data);
}
export async function deleteModelRoute(workspace: Workspace, route: string): Promise<Workspace> {
  const result = await api.DELETE('/api/v1/workspaces/{slug}/model-routes/{route}', {
    params: { path: { slug: workspace.slug, route }, header: { 'If-Match': `"${workspace.management_etag}"` } },
      });
  return dataOrFail(result.data as Workspace | undefined);
}
export async function getMCPMounts(slug: string): Promise<MCPMount[]> {
  const result = await api.GET('/api/v1/workspaces/{slug}/mcp-mounts', { params: { path: { slug } } });
  return dataOrFail(result.data as MCPMount[] | undefined);
}
export async function getMCPCatalog(id: string): Promise<MCPTool[]> {
  return dataOrFail((await api.GET('/api/v1/connections/{id}/mcp-catalog', { params: { path: { id } } })).data);
}
export async function previewMCPMount(workspace: Workspace, mount: MCPMount): Promise<MCPMountPreview> {
  const params = { path: { slug: workspace.slug, alias: mount.alias }, query: { dry_run: true },
    header: { 'If-Match': `"${workspace.management_etag}"` } };
  const result = await api.PUT('/api/v1/workspaces/{slug}/mcp-mounts/{alias}', { params, body: mount });
  return dataOrFail(result.data as MCPMountPreview | undefined);
}
export async function applyMCPMount(workspace: Workspace, mount: MCPMount, review: MCPMountPreview): Promise<Workspace> {
  const exact: MCPMountReview = { workspace_etag: review.revision, digest: review.digest,
    connection_revision: review.connection_revision, access_revision: review.access_revision,
    publication_revision: review.publication_revision };
  const params = { path: { slug: workspace.slug, alias: mount.alias }, query: { dry_run: false },
    header: { 'If-Match': `"${review.revision}"` } };
  const result = await api.PUT('/api/v1/workspaces/{slug}/mcp-mounts/{alias}', { params, body: { ...mount, review: exact } });
  return dataOrFail(result.data as Workspace | undefined);
}
export async function deleteMCPMount(workspace: Workspace, alias: string): Promise<Workspace> {
  const result = await api.DELETE('/api/v1/workspaces/{slug}/mcp-mounts/{alias}', {
    params: { path: { slug: workspace.slug, alias }, header: { 'If-Match': `"${workspace.management_etag}"` } },
  });
  return dataOrFail(result.data as Workspace | undefined);
}
export async function getSearchRoute(slug: string): Promise<SearchRoute | null> {
  try {
    const result = await api.GET('/api/v1/workspaces/{slug}/search-route', { params: { path: { slug } } });
    return dataOrFail(result.data as SearchRoute | undefined);
  } catch (error) {
    if (error instanceof ControlRequestError && error.status === 404) return null;
    throw error;
  }
}
export async function putSearchRoute(workspace: Workspace, route: SearchRoute): Promise<Workspace> {
  const result = await api.PUT('/api/v1/workspaces/{slug}/search-route', {
    params: { path: { slug: workspace.slug }, header: { 'If-Match': `"${workspace.management_etag}"` } },
    body: route,
  });
  return dataOrFail(result.data as Workspace | undefined);
}
export async function deleteSearchRoute(workspace: Workspace): Promise<Workspace> {
  const result = await api.DELETE('/api/v1/workspaces/{slug}/search-route', {
    params: { path: { slug: workspace.slug }, header: { 'If-Match': `"${workspace.management_etag}"` } },
  });
  return dataOrFail(result.data as Workspace | undefined);
}

export const queryKeys = {
  node: ['node'] as const,
  workspaces: ['workspaces'] as const,
  workspace: (slug: string) => ['workspace', slug] as const,
  services: (slug: string) => ['workspace-services', slug] as const,
  connections: ['connections'] as const,
  connection: (id: string) => ['connection', id] as const,
  references: (id: string) => ['connection-references', id] as const,
  route: (slug: string, name: string) => ['model-route', slug, name] as const,
  mounts: (slug: string) => ['mcp-mounts', slug] as const,
  catalog: (id: string) => ['mcp-catalog', id] as const,
  searchRoute: (slug: string) => ['search-route', slug] as const,
};

// Creation retry identity must be retained across a manual retry of the same
// intent. An edited draft must instead receive a new key from the form owner.
export function newIntentKey(): string { return crypto.randomUUID(); }
