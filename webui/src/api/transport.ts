import type { components } from './generated/schema';

export type ControlProblem = components['schemas']['ControlProblem'];
export class ControlRequestError extends Error {
  constructor(readonly status: number, readonly problem?: ControlProblem) {
    super(problem?.detail ?? problem?.title ?? `Request failed (${status}).`);
    this.name = 'ControlRequestError';
  }
}

/** The generated client owns request serialization and response DTOs. This
 * adapter owns browser authority and wire admission. No automatic retries or
 * revision/key regeneration occur here. Successful JSON must be no-store,
 * including one-shot credentials, before the client can read its body. */
export function createControlTransport(origin: string, fetcher: typeof fetch = fetch): typeof fetch {
  return async (input, init) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    if (url.origin !== origin || !url.pathname.startsWith('/api/v1/') || (url.pathname.includes('%') && !isCanonicalModelRoutePatternPath(url.pathname)) || url.pathname.includes('\\') || url.hash) {
      throw new Error('Expected a same-origin canonical control path.');
    }
    const etag = request.headers.get('If-Match');
    if (etag !== null && (!/^"[!-~]+"$/.test(etag) || /["\\,]/.test(etag.slice(1, -1)))) throw new Error('Expected one strong ETag.');
    const key = request.headers.get('Idempotency-Key');
    if (key !== null && !/^[!-~]{1,256}$/.test(key)) throw new Error('Expected one visible ASCII idempotency key.');
    const response = await fetcher(new Request(request, { credentials: 'same-origin', cache: 'no-store', redirect: 'error' }));
    if (!response.ok) {
      let problem: ControlProblem | undefined;
      if (response.headers.get('Content-Type')?.split(';')[0].trim() === 'application/problem+json') {
        try {
          const value: unknown = await response.clone().json();
          if (isControlProblem(value)) problem = value;
        } catch { /* Malformed wire failures retain HTTP status without body disclosure. */ }
      }
      throw new ControlRequestError(response.status, problem);
    }
    if (!response.headers.get('Cache-Control')?.split(',').some(value => value.trim().toLowerCase() === 'no-store')) {
      throw new Error('Control response is missing Cache-Control: no-store.');
    }
    return response;
  };
}

function isControlProblem(value: unknown): value is ControlProblem {
  if (typeof value !== 'object' || value === null) return false;
  const problem = value as Record<string, unknown>;
  return typeof problem.type === 'string' && typeof problem.title === 'string' && typeof problem.status === 'number' && typeof problem.code === 'string';
}

// Model patterns are public route identities, not filesystem paths. Standard
// URL encoding is needed for patterns such as deepseek/* and literal escapes.
// All other control paths retain the existing prohibition on percent escapes.
function isCanonicalModelRoutePatternPath(pathname: string): boolean {
  const match = /^\/api\/v1\/workspaces\/[a-z0-9-]+\/model-routes\/([^/]+)$/.exec(pathname);
  if (!match) return false;
  try {
    const pattern = decodeURIComponent(match[1]);
    return !!pattern && encodeURIComponent(pattern) === match[1] &&
      !pattern.split('/').some(segment => segment === '.' || segment === '..') &&
      !Array.from(pattern).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127);
  } catch {
    return false;
  }
}
