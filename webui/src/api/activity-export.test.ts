import { afterEach, expect, it, vi } from 'vitest';

const headers = {
  'Cache-Control': 'no-store', 'Content-Type': 'application/x-ndjson; charset=utf-8',
  'Content-Disposition': 'attachment; filename="swobu-activity-exchange-research.ndjson"',
  'X-Swobu-Export-Mode': 'retained-page', 'X-Swobu-Export-Rows': '1',
};
afterEach(() => { vi.unstubAllGlobals(); vi.resetModules(); });

it('downloads an exact filtered retained page through generated cookie Control', async () => {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('{"kind":"exchange"}\n', { headers: { ...headers, 'X-Swobu-Next-Cursor': 'opaque-next' } }));
  vi.stubGlobal('fetch', fetcher);
  const { downloadActivity } = await import('./activation');
  const file = await downloadActivity({ workspace: 'research', kind: 'exchange', from: '2026-10-20T12:00:00Z', to: '2026-10-21T12:00:00Z', principal: 'local', result: 'succeeded' }, 'ndjson', 201, 'opaque-current');
  expect(await file.blob.text()).toBe('{"kind":"exchange"}\n');
  expect(file.rows).toBe(1); expect(file.next).toBe('opaque-next');
  const request = fetcher.mock.calls[0][0] as Request;
  expect(Object.fromEntries(new URL(request.url).searchParams)).toEqual({ workspace: 'research', kind: 'exchange', format: 'ndjson', max_rows: '201', cursor: 'opaque-current', from: '2026-10-20T12:00:00Z', to: '2026-10-21T12:00:00Z', principal: 'local', result: 'succeeded' });
  expect(request.credentials).toBe('same-origin'); expect(request.redirect).toBe('error'); expect(request.cache).toBe('no-store');
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it('rejects a successful download without retained-page metadata', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('not evidence', { headers: { ...headers, 'X-Swobu-Export-Mode': 'complete-audit' } })));
  const { downloadActivity } = await import('./activation');
  await expect(downloadActivity({ workspace: 'research', kind: 'exchange' }, 'ndjson', 500)).rejects.toThrow('valid retained-history');
});

it('cancels a body that exceeds the server envelope before retaining it', async () => {
  const cancel = vi.fn();
  const stream = new ReadableStream<Uint8Array>({ start(controller) { controller.enqueue(new Uint8Array(8 * 1024 * 1024 + 1)); }, cancel });
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(stream, { headers })));
  const { downloadActivity } = await import('./activation');
  await expect(downloadActivity({ workspace: 'research', kind: 'exchange' }, 'ndjson', 500)).rejects.toThrow('8 MiB');
  expect(cancel).toHaveBeenCalledTimes(1);
});

it('retains failure status and never retries unavailable history', async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response('{}', { status: 503 }));
  vi.stubGlobal('fetch', fetcher);
  const { downloadActivity } = await import('./activation');
  await expect(downloadActivity({ workspace: 'research', kind: 'mcp_operation' }, 'csv', 500)).rejects.toMatchObject({ status: 503 });
  expect(fetcher).toHaveBeenCalledTimes(1);
});
