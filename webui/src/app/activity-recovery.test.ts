import { describe, expect, it } from 'vitest';
import { assessExchangeRecovery } from './activity-recovery';
import type { OperationDetail } from '../api/activation';

const create = (counts: [number, number, number, number, number], delivery_result = 'client_write_failed') => ({
  exchange_id: 'ex_example', access_id: 'local', workspace: 'research', started_at: '2026-10-21T00:00:00Z',
  upstream_execution: { recorded_invocations: counts[0], occurred: counts[1], may_have_occurred: counts[2], did_not_occur: counts[3], unsettled: counts[4] },
  delivery_result, invocations: [],
} satisfies NonNullable<OperationDetail['exchange']>);

describe('Activity recovery: consequences of observed evidence, never retry policy', () => {
  it('never treats absent records as proof no execution occurred', () => {
    const result = assessExchangeRecovery(create([0, 0, 0, 0, 0]));
    expect(result.attention).toBe(true);
    expect(result.explanation).toMatch(/does not prove/);
  });
  it('preserves an unsettled attempt without inventing a failure', () => {
    const result = assessExchangeRecovery(create([1, 0, 0, 0, 1], ''));
    expect(result.title).toMatch(/unresolved/);
    expect(result.next).toMatch(/before any replay/);
  });
  it('does not bury a possibly executed attempt behind a confirmed one', () => {
    const result = assessExchangeRecovery(create([2, 1, 1, 0, 0]));
    expect(result.title).toMatch(/uncertain/);
    expect(result.attention).toBe(true);
  });
  it('separates client delivery failure from upstream success', () => {
    const result = assessExchangeRecovery(create([1, 1, 0, 0, 0]));
    expect(result.title).toMatch(/client delivery failed/);
    expect(result.next).toMatch(/repeat work or charges/);
  });
  it('does not manufacture a recovery incident for successful delivery', () => {
    const result = assessExchangeRecovery(create([1, 1, 0, 0, 0], 'succeeded'));
    expect(result.attention).toBe(false);
  });
  it('does not claim that a recorded did-not-occur means safe global replay', () => {
    const result = assessExchangeRecovery(create([1, 0, 0, 1, 0]));
    expect(result.next).toMatch(/do not infer/);
  });
});
