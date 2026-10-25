import { afterEach, expect, it, vi } from 'vitest';

afterEach(() => { vi.unstubAllGlobals(); vi.resetModules(); });

const id = 'ex_AAAAAAAAAAAAAAAAAAAAAA';
function response(invocations: Array<Record<string, unknown>>, summary: Record<string, number>) {
  return new Response(JSON.stringify({ kind: 'exchange', evidence: 'ledger', exchange: {
    exchange_id: id, access_id: 'local', workspace: 'research', started_at: '2026-10-20T12:00:00Z',
    upstream_execution: summary, invocations,
  } }), { headers: { 'Cache-Control': 'no-store', 'Content-Type': 'application/json' } });
}

it('accepts mixed confirmed and uncertain recorded source facts without erasing uncertainty', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response([
    { invocation_id: 'ex_1_1', kind: 'model', started_at: '2026-10-20T12:00:01Z', execution_possibility: 'occurred' },
    { invocation_id: 'ex_1_2', kind: 'model', started_at: '2026-10-20T12:00:02Z', execution_possibility: 'may_have_occurred' },
  ], { recorded_invocations: 2, occurred: 1, may_have_occurred: 1, did_not_occur: 0, unsettled: 0 })));
  const { readOperation } = await import('./activation');
  const detail = await readOperation(id);
  expect(detail.exchange?.upstream_execution.may_have_occurred).toBe(1);
});

it('refuses mismatched upstream counts rather than displaying a fabricated certainty', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response([
    { invocation_id: 'ex_1_1', kind: 'model', started_at: '2026-10-20T12:00:01Z', execution_possibility: 'may_have_occurred' },
  ], { recorded_invocations: 1, occurred: 1, may_have_occurred: 0, did_not_occur: 0, unsettled: 0 })));
  const { readOperation } = await import('./activation');
  await expect(readOperation(id)).rejects.toThrow('disagrees with the recorded invocation');
});

it('does not infer no execution from a missing upstream summary', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response([], {})));
  const { readOperation } = await import('./activation');
  await expect(readOperation(id)).rejects.toThrow('disagrees with the recorded invocation');
});
