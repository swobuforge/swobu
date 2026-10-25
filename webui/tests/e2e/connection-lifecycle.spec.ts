import { test, expect } from '@playwright/test';
import { createServer } from 'node:http';

// Real embedded Operator and Control, with only the external provider synthetic.
// The lost acknowledgement occurs after the daemon has committed replacement.
test('shared Connection edits and exact credential replay preserve routes and change the next request', async ({ page, playwright }, info) => {
  const authorizations: string[] = [];
  const upstream = createServer(async (request, response) => {
    for await (const chunk of request) void chunk;
    authorizations.push(request.headers.authorization ?? '');
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: 'resp_lifecycle', object: 'response', status: 'completed', model: 'lifecycle-model', output: [], usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 } }));
  });
  await new Promise<void>(resolve => upstream.listen(0, '127.0.0.1', resolve));
  const address = upstream.address();
  if (!address || typeof address === 'string') throw new Error('upstream did not bind');
  const origin = 'http://127.0.0.1:4179';
  const endpoint = `http://127.0.0.1:${address.port}/v1`;
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  const rotated = 'synthetic-lifecycle-replacement';
  const consoleMessages: string[] = [];
  const secretWrites: { url: string; method: string }[] = [];
  page.on('request', request => {
    expect(request.url()).not.toContain(rotated);
    if (request.postData()?.includes(rotated)) secretWrites.push({ url: request.url(), method: request.method() });
  });
  page.on('console', message => consoleMessages.push(message.text()));
  page.on('pageerror', error => consoleMessages.push(error.message));
  try {
    const launch = await native.post(`${origin}/api/v1/browser-launches`);
    expect(launch.status()).toBe(201);
    await page.goto(origin + (await launch.json()).launch_path);
    await expect(page).toHaveURL(`${origin}/workspaces`);
    await page.goto(`${origin}/connections`);
    await page.getByRole('button', { name: 'New Connection' }).click();
    let dialog = page.getByRole('dialog');
    await dialog.getByLabel('Name', { exact: true }).fill('Lifecycle shared');
    await dialog.getByLabel('Provider (standard API)').fill('lmstudio');
    await dialog.getByLabel('Override provider endpoint (optional)').fill(endpoint);
    await dialog.getByLabel('Credential reference', { exact: true }).fill('env:SWOBU_WEB_PROOF_KEY');
    await dialog.getByRole('button', { name: 'Create Connection' }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page).toHaveURL(/\/connections\/cn_/);
    const id = new URL(page.url()).pathname.split('/').at(-1)!;
    const control = `${origin}/api/v1/connections/${id}`;
    const slugs = ['lifecycle-a', 'lifecycle-b'];
    const before = [];
    for (const slug of slugs) {
      expect((await native.post(`${origin}/api/v1/workspaces`, { headers: { 'Idempotency-Key': slug }, data: { slug } })).status()).toBe(201);
      const path = `${origin}/api/v1/workspaces/${slug}`;
      let workspace = await native.get(path);
      expect((await native.put(`${path}/services/models`, { headers: { 'If-Match': workspace.headers().etag } })).status()).toBe(200);
      workspace = await native.get(path);
      expect((await native.put(`${path}/model-routes/coding`, { headers: { 'If-Match': workspace.headers().etag }, data: {
        tiers: [{ targets: [{ id: 'capacity', connection_id: id, model: 'lifecycle-model', protocol: 'responses' }] }],
      } })).status()).toBe(200);
      before.push(await (await native.get(path)).json());
    }
    const requestModel = async (slug: string) => {
      const result = await native.post(`${origin}/workspaces/${slug}/v1/responses`, { data: { model: 'coding', input: 'synthetic lifecycle proof', stream: false } });
      expect(result.status()).toBe(200);
    };
    await requestModel(slugs[0]);
    expect(authorizations.at(-1)).toBe('Bearer synthetic-browser-proof');
    await page.reload();
    await page.getByRole('button', { name: 'Edit configuration' }).click();
    dialog = page.getByRole('dialog');
    await expect(dialog.getByLabel('Name', { exact: true })).toBeFocused();
    await dialog.getByLabel('Name', { exact: true }).fill('Lifecycle maintained');
    await dialog.getByLabel(/Endpoint/).fill(endpoint);
    await dialog.getByLabel(/Credential reference/).fill('env:SWOBU_WEB_PROOF_KEY');
    await dialog.getByRole('checkbox').check();
    for (const width of [390, 759, 760, 761, 1440]) {
      await page.setViewportSize({ width, height: 800 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await dialog.getByRole('button', { name: 'Save Connection' }).scrollIntoViewIfNeeded();
      await expect(dialog.getByRole('button', { name: 'Save Connection' })).toBeInViewport();
      await page.screenshot({ path: info.outputPath(`edit-${width}.png`), fullPage: true });
    }
    await dialog.getByRole('button', { name: 'Save Connection' }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Lifecycle maintained' })).toBeVisible();
    const edited = await (await native.get(control)).json();
    expect(edited.id).toBe(id);
    expect(edited.revision).toBe('2');
    let calls = 0;
    let firstBody = '';
    let firstKey = '';
    await page.route(`${control}/credential-replacements`, async route => {
      const request = route.request();
      calls++;
      expect(request.headers()['if-match']).toBe('"2"');
      if (calls === 1) {
        firstBody = request.postData()!;
        firstKey = request.headers()['idempotency-key'];
      } else {
        expect(request.postData()).toBe(firstBody);
        expect(request.headers()['idempotency-key']).toBe(firstKey);
      }
      const response = await route.fetch();
      expect(response.status()).toBe(200);
      const receipt = await response.json();
      expect(receipt.connection.revision).toBe('3');
      expect(receipt.replayed).toBe(calls === 2);
      expect(await response.text()).not.toContain(rotated);
      if (calls === 1) await route.abort('failed');
      else await route.fulfill({ response });
    });
    await page.getByRole('button', { name: 'Replace credential' }).click();
    dialog = page.getByRole('dialog');
    await expect(dialog.getByLabel('New secret')).toBeFocused();
    await dialog.getByLabel('New secret').fill(rotated);
    await dialog.getByRole('checkbox').check();
    for (const width of [390, 759, 760, 761, 1440]) {
      await page.setViewportSize({ width, height: 800 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath(`replace-${width}.png`), fullPage: true });
    }
    await dialog.getByRole('button', { name: 'Replace credential', exact: true }).click();
    await expect(dialog.getByRole('button', { name: 'Retry exact replacement' })).toBeVisible();
    expect(calls).toBe(1);
    await expect(dialog.getByLabel('New secret')).toBeDisabled();
    await dialog.getByRole('button', { name: 'Retry exact replacement' }).click();
    await expect(dialog).toHaveCount(0);
    expect(calls).toBe(2);
    expect(secretWrites).toEqual(Array.from({ length: 2 }, () => ({ url: `${control}/credential-replacements`, method: 'POST' })));
    expect((await (await native.get(control)).json()).revision).toBe('3');
    for (const [index, slug] of slugs.entries()) {
      expect(await (await native.get(`${origin}/api/v1/workspaces/${slug}`)).json()).toEqual(before[index]);
      await requestModel(slug);
      expect(authorizations.at(-1)).toBe(`Bearer ${rotated}`);
    }
    const refs = await (await native.get(`${control}/references`)).json();
    expect(refs.consumers).toHaveLength(2);
    for (const path of [control, `${origin}/api/v1/connections`, `${origin}/api/v1/activity?kind=exchange&limit=50`]) {
      const body = await (await native.get(path)).text();
      expect(body).not.toContain(rotated);
      expect(body).not.toContain('synthetic-browser-proof');
      expect(body).not.toContain('SWOBU_WEB_PROOF_KEY');
    }
    // Model-visible administration consumes the same redacted Connection query.
    const inspector = `${origin}/api/v1/workspaces/lifecycle-inspector`;
    expect((await native.post(`${origin}/api/v1/workspaces`, { headers: { 'Idempotency-Key': 'lifecycle-inspector' }, data: { slug: 'lifecycle-inspector' } })).status()).toBe(201);
    const inspectorState = await native.get(inspector);
    expect((await native.put(`${inspector}/services/mcp`, { headers: { 'If-Match': inspectorState.headers().etag } })).status()).toBe(200);
    await page.goto(`${origin}/workspaces/lifecycle-inspector/services/mcp`);
    await page.getByRole('button', { name: 'Add server' }).click();
    dialog = page.getByRole('dialog');
    await dialog.getByLabel('Namespace').fill('node');
    await dialog.getByLabel('MCP source').selectOption('builtin:swobu-admin');
    await dialog.getByLabel('Selected Admin tool names').fill('connection_get');
    await dialog.getByRole('button', { name: 'Review source & tools' }).click();
    await dialog.getByRole('button', { name: 'Apply reviewed mount' }).click();
    await expect(dialog).toHaveCount(0);
    const toolResponse = await native.post(`${origin}/workspaces/lifecycle-inspector/mcp`, { data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'node__connection_get', arguments: { id } } } });
    expect(toolResponse.status()).toBe(200);
    const toolWire = await toolResponse.text();
    expect(toolWire).toContain('Lifecycle maintained');
    expect(toolWire).not.toContain(rotated);
    expect(toolWire).not.toContain('synthetic-browser-proof');
    expect(toolWire).not.toContain('SWOBU_WEB_PROOF_KEY');
    expect(await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))).not.toContain(rotated);
    expect(consoleMessages.join('\n')).not.toContain(rotated);
    expect(await page.content()).not.toContain(rotated);
  } finally {
    await native.dispose();
    await new Promise<void>((resolve, reject) => upstream.close(error => error ? reject(error) : resolve()));
  }
});
