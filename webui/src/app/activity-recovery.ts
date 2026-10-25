import type { OperationDetail } from '../api/activation';

type Exchange = NonNullable<OperationDetail['exchange']>;

export type RecoveryAssessment = { title: string; explanation: string; next: string; attention: boolean };

/** Evidence interpretation only; this never authorizes an automatic replay.
 * Missing invocation facts do not prove a request never reached a provider. */
export function assessExchangeRecovery(exchange: Exchange): RecoveryAssessment {
  const evidence = exchange.upstream_execution;
  if (evidence.recorded_invocations === 0) return {
    title: 'No upstream invocation recorded',
    explanation: 'Activity has no source invocation for this Exchange. This does not prove that nothing was dispatched.',
    next: 'Inspect the route and any external source state before deciding whether to submit another request.',
    attention: true,
  };
  if (evidence.unsettled > 0 || !exchange.delivery_result) return {
    title: 'Execution still unresolved',
    explanation: `${evidence.unsettled} recorded invocation${evidence.unsettled === 1 ? '' : 's'} lack${evidence.unsettled === 1 ? 's' : ''} a terminal observation, or the client delivery is not settled.`,
    next: 'Inspect current Activity and the upstream system before any replay. A missing terminal is not a failed execution.',
    attention: true,
  };
  if (evidence.may_have_occurred > 0) return {
    title: 'Upstream execution uncertain',
    explanation: `At least ${evidence.may_have_occurred} recorded invocation${evidence.may_have_occurred === 1 ? ' may' : 's may'} have executed. A separate successful attempt does not resolve that uncertainty.`,
    next: 'Check upstream effects, provider usage and idempotency before replaying; Activity cannot establish duplicate safety or charges.',
    attention: true,
  };
  if (evidence.occurred > 0) {
    const deliveryFailed = exchange.delivery_result !== 'succeeded';
    return {
      title: deliveryFailed ? 'Upstream execution confirmed; client delivery failed' : 'Upstream execution recorded',
      explanation: deliveryFailed
        ? 'At least one upstream execution occurred, but client delivery did not succeed. The client failure does not undo upstream work.'
        : 'At least one upstream invocation is recorded as occurred, and client delivery succeeded.',
      next: deliveryFailed
        ? 'Inspect the recorded route and upstream effects before retrying. A second request could repeat work or charges.'
        : 'No recovery action is established by this record.',
      attention: deliveryFailed,
    };
  }
  return {
    title: 'Recorded invocations did not occur',
    explanation: 'All recorded invocations report that execution did not occur. Activity is not a complete dispatch or audit log.',
    next: 'Use the failure code and recorded route to diagnose why the request did not complete; do not infer a universally safe replay.',
    attention: exchange.delivery_result !== 'succeeded',
  };
}
