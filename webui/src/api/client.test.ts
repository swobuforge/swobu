import { expect, it, vi } from 'vitest';
import { createControlClient } from './client';

it('generated client binds paths, headers and cookie transport', async () => {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ type: 'urn:conflict', title: 'Changed', status: 412, code: 'REVISION_CONFLICT' }), { status: 412, headers: { 'Content-Type': 'application/problem+json' } }));
  const client = createControlClient(fetcher);
  const result = client.PATCH('/api/v1/workspaces/{slug}', { params: { path: { slug: 'research' }, header: { 'If-Match': '"original"' } }, body: { display_name: 'Research' } });
  await expect(result).rejects.toMatchObject({ status: 412, problem: { code: 'REVISION_CONFLICT' } });
  const request = fetcher.mock.calls[0][0] as Request;
  expect(request.url).toContain('/api/v1/workspaces/research');
  expect(request.credentials).toBe('same-origin');
  expect(request.cache).toBe('no-store');
  expect(request.redirect).toBe('error');
  expect(request.headers.get('If-Match')).toBe('"original"');
});
