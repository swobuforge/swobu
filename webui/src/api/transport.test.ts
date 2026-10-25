import { describe, expect, it, vi } from 'vitest';
import { ControlRequestError, createControlTransport } from './transport';

const origin = 'http://localhost';
describe('canonical control transport', () => {
  it('preserves original consent and retry identity without retries', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('{}', { headers: { 'Cache-Control': 'no-store', ETag: '"next"' } }));
    const transport = createControlTransport(origin, fetcher);
    const result = await transport(`${origin}/api/v1/workspaces/research`, { method: 'PATCH', headers: { 'If-Match': '"original"', 'Idempotency-Key': 'intent-1' } });
    expect(fetcher).toHaveBeenCalledTimes(1);
    const request = fetcher.mock.calls[0][0] as Request;
    expect(request.credentials).toBe('same-origin');
    expect(request.cache).toBe('no-store');
    expect(request.redirect).toBe('error');
    expect(request.headers.get('If-Match')).toBe('"original"');
    expect(request.headers.get('Idempotency-Key')).toBe('intent-1');
    expect(result.headers.get('ETag')).toBe('"next"');
  });
  it('decodes Problems and keeps unstructured failures generic', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(new Response(JSON.stringify({ type: 'urn:conflict', title: 'Changed', status: 412, code: 'REVISION_CONFLICT', detail: 'Reload.' }), { status: 412, headers: { 'Content-Type': 'application/problem+json' } })).mockResolvedValueOnce(new Response('private body', { status: 401 }));
    const transport = createControlTransport(origin, fetcher);
    await expect(transport(`${origin}/api/v1/node`)).rejects.toMatchObject({ status: 412, message: 'Reload.' });
    await expect(transport(`${origin}/api/v1/node`)).rejects.toBeInstanceOf(ControlRequestError);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
  it('requires no-store before exposing successful response bodies', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('{}'));
    await expect(createControlTransport(origin, fetcher)(`${origin}/api/v1/connections`)).rejects.toThrow('no-store');
  });
  it('permits only canonical escaped model-pattern identity segments', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('{}', { headers: { 'Cache-Control': 'no-store' } }));
    const transport = createControlTransport(origin, fetcher);
    for (const path of [
      '/api/v1/workspaces/dev/model-routes/deepseek%2F*',
      '/api/v1/workspaces/dev/model-routes/gpt-%5B45%5D',
      '/api/v1/workspaces/dev/model-routes/literal%5C*',
    ]) await transport(origin + path);
    for (const path of [
      '/api/v1/workspaces/dev/model-match%2Fsecret',
      '/api/v1/workspaces/dev/model-routes/invalid%2fpath',
      '/api/v1/workspaces/dev/model-routes/trailing%XY',
      '/api/v1/workspaces/dev/model-routes/%2E%2E%2Fescape',
    ]) await expect(transport(origin + path)).rejects.toThrow();
    expect(fetcher).toHaveBeenCalledTimes(3);
  });
  it('rejects remote/legacy paths and malformed consent before fetch', async () => {
    const fetcher = vi.fn<typeof fetch>();
    const transport = createControlTransport(origin, fetcher);
    for (const path of ['https://remote/api/v1/node', `${origin}/_swobu/status`, `${origin}/api/v1/%2f/node`]) await expect(transport(path)).rejects.toThrow();
    await expect(transport(`${origin}/api/v1/node`, { headers: { 'If-Match': 'W/"revision"' } })).rejects.toThrow();
    await expect(transport(`${origin}/api/v1/node`, { headers: { 'Idempotency-Key': 'two words' } })).rejects.toThrow();
    expect(fetcher).not.toHaveBeenCalled();
  });
});
