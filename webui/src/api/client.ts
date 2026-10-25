import createClient from 'openapi-fetch';
import { createControlTransport } from './transport';
import type { paths } from './generated/schema';

/** Generated paths/methods/parameters constrain requests. Domain cards supply
 * success schemas before resource projections can be consumed as typed data. */
export function createControlClient(fetcher: typeof fetch = fetch) {
  const origin = typeof location === 'undefined' ? 'http://localhost' : location.origin;
  return createClient<paths>({
    baseUrl: origin,
    credentials: 'same-origin', cache: 'no-store', redirect: 'error',
    fetch: createControlTransport(origin, fetcher),
  });
}
