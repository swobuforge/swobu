import type { components } from './generated/schema';
import { createControlClient } from './client';

export type ServiceLimits = components['schemas']['WorkspaceServiceLimits'];
export type ServiceLimitsSnapshot = { limits: ServiceLimits; etag: string };
const api = createControlClient();

function snapshot(data: ServiceLimits | undefined, response: Response): ServiceLimitsSnapshot {
  const etag = response.headers.get('ETag');
  if (!data || !etag || !/^"[^"\s,]+"$/.test(etag)) throw new Error('Node did not supply a strong Workspace revision.');
  for (const service of ['models', 'mcp', 'search'] as const) {
    const quota = data[service];
    if (!quota || !Number.isSafeInteger(quota.requests_per_minute) || quota.requests_per_minute < 0 || quota.requests_per_minute > 100000 ||
        (quota.concurrent !== undefined && (!Number.isSafeInteger(quota.concurrent) || quota.concurrent < 0 || quota.concurrent > (service === 'models' ? 10000 : 0)))) {
      throw new Error('Node returned invalid Workspace operational limits.');
    }
  }
  return { limits: data, etag };
}
export async function getServiceLimits(slug: string): Promise<ServiceLimitsSnapshot> {
  const result = await api.GET('/api/v1/workspaces/{slug}/service-limits', { params: { path: { slug } } });
  return snapshot(result.data, result.response);
}
export async function setServiceLimits(slug: string, current: ServiceLimitsSnapshot, limits: ServiceLimits): Promise<ServiceLimitsSnapshot> {
  const result = await api.PUT('/api/v1/workspaces/{slug}/service-limits', {
    params: { path: { slug }, header: { 'If-Match': current.etag } }, body: limits,
  });
  const saved = snapshot(result.data, result.response);
  for (const kind of ['models', 'mcp', 'search'] as const) {
    if (saved.limits[kind].requests_per_minute !== limits[kind].requests_per_minute || (saved.limits[kind].concurrent ?? 0) !== (limits[kind].concurrent ?? 0)) {
      throw new Error('Saved limits differ from the submitted policy. Read current state before another change.');
    }
  }
  return saved;
}
