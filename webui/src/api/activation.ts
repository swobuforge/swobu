import { createControlClient } from './client';
import { newIntentKey } from './resources';
import type { operations } from './generated/schema';

// The server's apischema.Connect/Activity contracts are the wire authority.
// This boundary is deliberately the only temporary assertion point until Bun
// regenerates src/api/generated/schema.ts from the new Go OpenAPI document.
const api = createControlClient();
function present<T>(value: T | undefined): T {
  if (value === undefined) throw new Error('Node did not return a resource.');
  return value;
}

export type DetectedClient = { id: string; name: string };
export type ClientChange = { field: string; before: string; before_exists: boolean; after: string };
export type ClientAuthentication = { principal: string; permission_revision: string; issue_credential: boolean; credential_id?: string };
export type ClientReview = { authentication: ClientAuthentication; revision: string; config_paths: string[]; changes: ClientChange[] };
export type ClientPlan = ClientReview & {
  workspace: string; model_id: string; client: DetectedClient; post_apply_hint?: string;
  already_configured: boolean; requires_replace: boolean;
};
export type ClientApply = ClientPlan & { replayed: boolean };

export async function discoverClients(workspace: string): Promise<DetectedClient[]> {
  const result = await api.GET('/api/v1/clients' as never, { params: { query: { workspace } } } as never);
  const clients = present(result.data as DetectedClient[] | undefined);
  if (!Array.isArray(clients) || clients.some(c => !c || typeof c.id !== 'string' || typeof c.name !== 'string')) {
    throw new Error('Invalid local client discovery acknowledgement.');
  }
  return clients;
}

function validClientAuthentication(auth: ClientAuthentication | undefined, principal: string): boolean {
 return !!auth && !!principal && auth.principal === principal && typeof auth.permission_revision === 'string' && !!auth.permission_revision &&
 typeof auth.issue_credential === 'boolean' && (auth.issue_credential ? !auth.credential_id : typeof auth.credential_id === 'string' && !!auth.credential_id);
}

export async function previewClient(workspace: string, client: string, principal: string, modelID: string): Promise<ClientPlan> {
  const result = await api.POST('/api/v1/client-configurations' as never, {
    params: { query: { dry_run: true } }, body: { workspace, model_id: modelID, client, principal },
  } as never);
  const plan = present(result.data as ClientPlan | undefined);
  if (plan.workspace !== workspace || plan.model_id !== modelID || plan.client?.id !== client || !validClientAuthentication(plan.authentication, principal) ||
      !/^connect-[0-9a-f]{64}$/.test(plan.revision) ||
      !Array.isArray(plan.config_paths) || !Array.isArray(plan.changes) ||
      typeof plan.already_configured !== 'boolean' || typeof plan.requires_replace !== 'boolean' ||
      plan.changes.some(c => !c || !c.field || typeof c.before !== 'string' || typeof c.after !== 'string' || typeof c.before_exists !== 'boolean')) {
    throw new Error('Connect review is incomplete or belongs to another client or Workspace.');
  }
  return plan;
}

// Browser approval is an explicit user action. Callers retain this exact plan
// and the same intent key until the response has been reconciled; do not refresh
// the plan or generate a new key within this method.
export async function applyClient(plan: ClientPlan, intent: string): Promise<ClientApply> {
  const review: ClientReview = {
    authentication: plan.authentication, revision: plan.revision, config_paths: plan.config_paths, changes: plan.changes,
  };
  const result = await api.POST('/api/v1/client-configurations' as never, {
    params: { query: { dry_run: false }, header: {
      'If-Match': `"${plan.revision}"`, 'Idempotency-Key': intent,
    } },
    body: { workspace: plan.workspace, model_id: plan.model_id, client: plan.client.id, principal: plan.authentication.principal, review },
  } as never);
  const outcome = present(result.data as ClientApply | undefined);
  if (outcome.workspace !== plan.workspace || outcome.model_id !== plan.model_id || outcome.client?.id !== plan.client.id || !validClientAuthentication(outcome.authentication, plan.authentication.principal) ||
      outcome.authentication.issue_credential ||
      !Array.isArray(outcome.changes) || outcome.changes.length !== 0 ||
      !outcome.already_configured || outcome.requires_replace ||
      typeof outcome.replayed !== 'boolean' || !/^connect-[0-9a-f]{64}$/.test(outcome.revision)) {
    throw new Error('Connect outcome cannot be verified. Inspect the local client configuration before any further action.');
  }
  return outcome;
}
export { newIntentKey };

export type ActivityExchange = {
  exchange_id: string; access_id: string; workspace: string; started_at: string;
  state: string; attempt_count: number; operation?: string; route?: string; provider?: string;
  model?: string; client_family?: string; client_protocol?: string; http_status?: number;
  duration_ms?: number | null; connection_id?: string; connection_revision?: number;
  token_usage?: { input_tokens?: number; output_tokens?: number; reasoning_tokens?: number; cache_read_tokens?: number; cache_write_tokens?: number };
};
export type ActivityMCP = {
  operation_id: string; access_id: string; workspace: string; source: string; tool: string;
  started_at: string; settled_at?: string; outcome?: string; execution_possibility?: string;
};
export type ActivityProjection = {
  scope: { kind: 'all' | 'workspace'; workspace?: string };
  exchanges: ActivityExchange[]; mcp_operations?: ActivityMCP[];
  history?: { state: string; queue_depth: number; queue_capacity: number; dropped_exchanges_total: number; dropped_mcp_operations_total: number };
  page?: { mode: string; limit: number; exchange_next?: string; mcp_next?: string };
};
export type ActivityKind = 'all' | 'exchange' | 'mcp_operation';
export type ActivityFilter = { workspace: string; kind: ActivityKind; cursor?: string; from?: string; to?: string; principal?: string; result?: string };
export type ActivitySearch = Partial<ActivityFilter>;
export const activityPageSize = 50;

export async function readActivity(filter: ActivityFilter): Promise<ActivityProjection> {
  const paged = filter.kind !== 'all';
  const query: Record<string, string | number> = {};
  if (filter.workspace) query.workspace = filter.workspace;
  if (paged) {
    query.kind = filter.kind;
    query.limit = activityPageSize;
    if (filter.cursor) query[filter.kind === 'exchange' ? 'exchange_cursor' : 'mcp_cursor'] = filter.cursor;
  }
  if (filter.kind === 'exchange') {
    for (const key of ['from', 'to', 'principal', 'result'] as const) if (filter[key]) query[key] = filter[key];
  }
  const result = await api.GET('/api/v1/activity', { params: { query } } as never);
  const projection = present(result.data as ActivityProjection | undefined);
  const scope = filter.workspace ? 'workspace' : 'all';
  if (projection.scope?.kind !== scope || (filter.workspace && projection.scope.workspace !== filter.workspace) ||
      !Array.isArray(projection.exchanges) || (projection.mcp_operations !== undefined && !Array.isArray(projection.mcp_operations)) ||
      projection.exchanges.some(x => filter.workspace && x.workspace !== filter.workspace) ||
      projection.mcp_operations?.some(x => filter.workspace && x.workspace !== filter.workspace) ||
      (filter.kind === 'exchange' && (projection.mcp_operations?.length ?? 0) > 0) ||
      (filter.kind === 'mcp_operation' && projection.exchanges.length > 0) ||
      (filter.workspace && projection.history !== undefined)) {
    throw new Error('Activity evidence does not match the requested scope.');
  }
  if (paged && (projection.page?.mode !== 'retained' || projection.page.limit !== activityPageSize ||
      projection.exchanges.length > activityPageSize || (projection.mcp_operations?.length ?? 0) > activityPageSize)) {
    throw new Error('Invalid retained Activity page acknowledgement.');
  }
  if (paged) {
    const next = filter.kind === 'exchange' ? projection.page?.exchange_next : projection.page?.mcp_next;
    const count = filter.kind === 'exchange' ? projection.exchanges.length : projection.mcp_operations?.length ?? 0;
    if (next && (count === 0 || next === filter.cursor)) throw new Error('Invalid Activity continuation cursor.');
  }
  if (!paged && projection.page) throw new Error('Unexpected Activity pagination.');
  return projection;
}

export type Invocation = {
  invocation_id: string; kind: string; started_at: string; settled_at?: string;
  source?: string; tool?: string; provider?: string; route?: string; model?: string; target?: string;
  connection_id?: string; connection_revision?: number; target_version?: number;
  outcome?: string; execution_possibility?: string; failure?: string; purpose?: string;
};
export type OperationDetail = {
  kind: 'exchange' | 'mcp_operation'; evidence: string;
  exchange?: { exchange_id: string; access_id: string; workspace: string; started_at: string; settled_at?: string; delivery_result?: string; error_code?: string; http_status?: number;
    upstream_execution: { recorded_invocations: number; occurred: number; may_have_occurred: number; did_not_occur: number; unsettled: number }; invocations: Invocation[] };
  mcp_operation?: ActivityMCP;
};
export async function readOperation(id: string): Promise<OperationDetail> {
  if (!/^(ex_[a-zA-Z0-9_-]+|mcp_[a-zA-Z0-9_-]+)$/.test(id)) {
    throw new Error('Invalid Activity identity.');
  }
  const result = await api.GET('/api/v1/activity/operations/{id}', { params: { path: { id } } });
  const detail = present(result.data as OperationDetail | undefined);
  if ((detail.kind === 'exchange' && (detail.exchange?.exchange_id !== id || !Array.isArray(detail.exchange.invocations))) ||
      (detail.kind === 'mcp_operation' && detail.mcp_operation?.operation_id !== id) ||
      !['exchange', 'mcp_operation'].includes(detail.kind)) {
    throw new Error('Activity detail has an inconsistent identity.');
  }
  if (detail.kind === 'exchange' && detail.exchange) {
    const upstream = detail.exchange.upstream_execution;
    const counts = detail.exchange.invocations.reduce((summary, invocation) => {
      if (!invocation.execution_possibility) summary.unsettled++;
      else if (invocation.execution_possibility === 'occurred') summary.occurred++;
      else if (invocation.execution_possibility === 'may_have_occurred') summary.may_have_occurred++;
      else if (invocation.execution_possibility === 'did_not_occur') summary.did_not_occur++;
      else summary.unrecognized++;
      return summary;
    }, { occurred: 0, may_have_occurred: 0, did_not_occur: 0, unsettled: 0, unrecognized: 0 });
    if (!upstream || counts.unrecognized ||
        Object.values(upstream).some(n => !Number.isSafeInteger(n) || n < 0) ||
        upstream.recorded_invocations !== detail.exchange.invocations.length ||
        upstream.occurred !== counts.occurred || upstream.may_have_occurred !== counts.may_have_occurred ||
        upstream.did_not_occur !== counts.did_not_occur || upstream.unsettled !== counts.unsettled) {
      throw new Error('Activity execution summary disagrees with the recorded invocation evidence.');
    }
  }
  return detail;
}


export type ActivityExportFormat = 'ndjson' | 'csv';
export type ActivityDownload = { blob: Blob; filename: string; rows: number; next?: string };

/** Download one bounded retained slice through the generated Control client.
 * Continuation always repeats the original filters; no implicit retry or
 * aggregation into an unbounded browser-side backup occurs. */
export async function downloadActivity(filter: ActivityFilter, format: ActivityExportFormat, maxRows: number, cursor?: string, signal?: AbortSignal): Promise<ActivityDownload> {
  if (filter.kind === 'all') throw new Error('Choose retained Exchanges or MCP operations before downloading.');
  const query = { kind: filter.kind, format, max_rows: maxRows, cursor, workspace: filter.workspace || undefined,
    ...(filter.kind === 'exchange' ? { from: filter.from, to: filter.to, principal: filter.principal, result: filter.result as NonNullable<operations['get_api_v1_activity_export']['parameters']['query']>['result'] } : {}) };
  const { data, response } = await api.GET('/api/v1/activity/export', { params: { query }, parseAs: 'stream', signal });
  const filename = `swobu-activity-${filter.kind}${filter.workspace ? `-${filter.workspace}` : ''}.${format}`;
  const type = format === 'csv' ? 'text/csv' : 'application/x-ndjson';
  const rows = response.headers.get('X-Swobu-Export-Rows') ?? '';
  const next = response.headers.get('X-Swobu-Next-Cursor') ?? undefined;
  const valid = response.headers.get('X-Swobu-Export-Mode') === 'retained-page' &&
    response.headers.get('Content-Type')?.split(';')[0].trim() === type &&
    response.headers.get('Content-Disposition') === `attachment; filename="${filename}"` &&
    /^(0|[1-9][0-9]*)$/.test(rows) && Number(rows) <= maxRows && (!next || (next.length <= 2048 && Number(rows) > 0));
  if (!valid || !data) {
    await data?.cancel();
    throw new Error('The Node did not return a valid retained-history download.');
  }
  const reader = data.getReader();
  const parts: ArrayBuffer[] = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      // Match the server's explicit export envelope before retaining a chunk.
      if (size > 8 * 1024 * 1024) {
        await reader.cancel();
        throw new Error('Download exceeded 8 MiB. Reduce rows per file and try again.');
      }
      parts.push(value.slice().buffer as ArrayBuffer);
    }
    signal?.throwIfAborted();
    return { blob: new Blob(parts, { type }), filename, rows: Number(rows), next };
  } finally { reader.releaseLock(); }
}
