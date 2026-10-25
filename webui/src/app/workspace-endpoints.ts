// A Workspace publication supplies reachability, never a client credential.
// Keep endpoint construction in one place for local and published views.
export type WorkspaceServiceKind = 'models' | 'mcp' | 'search';
export type WorkspaceEndpoint = { id: string; service: WorkspaceServiceKind; label: string; url: string };

const endpoints: ReadonlyArray<{ id: string; service: WorkspaceServiceKind; label: string; suffix: string }> = [
  { id: 'models-root', service: 'models', label: 'Workspace root (Codex / Claude Code)', suffix: '' },
  { id: 'models-v1', service: 'models', label: 'OpenAI-compatible API', suffix: '/v1' },
  { id: 'mcp', service: 'mcp', label: 'MCP endpoint', suffix: '/mcp' },
  { id: 'search', service: 'search', label: 'Direct Search', suffix: '/search' },
];

// The Node advertises an origin, not a fully specified Workspace URL. In
// particular, never fabricate a remote URL from the local browser origin.
export function workspaceEndpoints(origin: string, slug: string, services: readonly string[], remote = false): WorkspaceEndpoint[] {
  if (!slug || !origin) return [];
  let url: URL;
  try { url = new URL(origin); } catch { return []; }
  if (url.username || url.password || url.search || url.hash || url.pathname !== '/' ||
      (remote ? url.protocol !== 'https:' : !['http:', 'https:'].includes(url.protocol))) return [];
  const prefix = `${url.origin}/workspaces/${encodeURIComponent(slug)}`;
  return endpoints.filter(item => services.includes(item.service)).map(item => ({
    id: item.id, service: item.service, label: item.label, url: prefix + item.suffix,
  }));
}
