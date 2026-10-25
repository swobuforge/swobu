import { test, expect } from '@playwright/test';
import { createServer } from 'node:http';

test('ordered glob routes use the production matcher, encoded identities and one winning route', async ({ page, playwright }, testInfo) => {
  const models: string[] = [];
  const upstream = createServer(async (request, response) => {
    let body = '';
    for await (const chunk of request) body += String(chunk);
    const model = (JSON.parse(body) as { model: string }).model;
    models.push(model);
    response.writeHead(model === 'fixed-glob' ? 503 : 200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify(model === 'fixed-glob' ? { error: { message: 'unavailable' } } : {
      id: 'resp_priority', object: 'response', status: 'completed', model, output: [], usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 },
    }));
  });
  await new Promise<void>(resolve => upstream.listen(0, '127.0.0.1', resolve));
  const address = upstream.address();
  if (!address || typeof address === 'string') throw new Error('upstream did not bind');
  const origin = 'http://127.0.0.1:4179';
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  const control = `${origin}/api/v1/workspaces/priority-proof`;
  try {
    const created = await native.post(`${origin}/api/v1/workspaces`, { headers: { 'Idempotency-Key': 'priority-workspace' }, data: { slug: 'priority-proof' } });
    expect(created.status()).toBe(201);
    const connection = await native.post(`${origin}/api/v1/connections`, { headers: { 'Idempotency-Key': 'priority-connection' }, data: {
      display_name: 'Priority capacity', kind: 'model', spec: { shape: 'standard', provider_id: 'lmstudio', configuration: {
        endpoint: `http://127.0.0.1:${address.port}/v1`, credential_ref: 'env:SWOBU_WEB_PROOF_KEY',
      } },
    } });
    expect(connection.status()).toBe(201);
    const connectionID = (await connection.json()).connection.id as string;
    let current = await (await native.get(control)).json();
    const enable = await native.put(`${control}/services/models`, { headers: { 'If-Match': `"${current.management_etag}"` } });
    expect(enable.status()).toBe(200);
    const launch = await (await native.post(`${origin}/api/v1/browser-launches`)).json();
    await page.goto(origin + launch.launch_path);
    await expect(page).toHaveURL(`${origin}/workspaces`);
    await page.goto(`${origin}/workspaces/priority-proof/services/models`);
    for (const pattern of ['coding', 'claude-sonnet-*', 'deepseek/*', '*']) {
      await page.getByRole('button', { name: 'New route', exact: true }).click();
      const dialog = page.getByRole('dialog');
      await dialog.getByLabel('Model pattern').fill(pattern);
      await dialog.getByLabel('Target ID', { exact: true }).fill(`target-${['coding', 'claude-sonnet-*', 'deepseek/*', '*'].indexOf(pattern)}`);
      await dialog.getByRole('combobox', { name: 'Connection', exact: true }).selectOption(connectionID);
      await dialog.getByLabel('Upstream model').fill(pattern === 'claude-sonnet-*' ? 'fixed-glob' : 'fixed-other');
      await dialog.getByRole('button', { name: 'Create route', exact: true }).click();
      await expect(dialog).toHaveCount(0);
    }
    await page.getByRole('button', { name: 'New route', exact: true }).click();
    const invalidDraft = page.getByRole('dialog');
    await invalidDraft.getByLabel('Model pattern').fill('[');
    await invalidDraft.getByLabel('Target ID', { exact: true }).fill('invalid-target');
    await invalidDraft.getByRole('combobox', { name: 'Connection', exact: true }).selectOption(connectionID);
    await invalidDraft.getByLabel('Upstream model').fill('fixed-other');
    const invalidSave = page.waitForResponse(response => response.request().method() === 'PUT' && response.url().includes('/model-routes/'));
    await invalidDraft.getByRole('button', { name: 'Create route', exact: true }).click();
    expect((await invalidSave).status()).toBe(400);
    await expect(invalidDraft.getByRole('alert')).toContainText('draft is preserved');
    await expect(invalidDraft.getByLabel('Model pattern')).toHaveValue('[');
    await invalidDraft.getByRole('button', { name: 'Cancel', exact: true }).click();
    const saved = await (await native.get(control)).json();
    expect(saved.routes.map((route: { name: string }) => route.name)).toEqual(['coding', 'claude-sonnet-*', 'deepseek/*', '*']);
    expect(saved.routes.every((route: { tiers: { targets: unknown[] }[] }) => route.tiers.length && route.tiers.every(tier => tier.targets.length))).toBe(true);
    await page.getByRole('button', { name: 'Move * up', exact: true }).focus();
    await page.keyboard.press('Enter');
    await expect(page.getByText('Unapplied priority', { exact: true })).toBeVisible();
    expect((await (await native.get(control)).json()).routes.map((route: { name: string }) => route.name)).toEqual(saved.routes.map((route: { name: string }) => route.name));
    await page.getByRole('button', { name: 'Apply priority', exact: true }).click();
    await expect(page.getByText('Unapplied priority', { exact: true })).toHaveCount(0);
    current = await (await native.get(control)).json();
    expect(current.routes.map((route: { name: string }) => route.name)).toEqual(['coding', 'claude-sonnet-*', '*', 'deepseek/*']);
    const stale = await native.patch(`${control}/model-routes`, { headers: { 'If-Match': `"${saved.management_etag}"` }, data: { order: saved.routes.map((route: { name: string }) => route.name) } });
    expect(stale.status()).toBe(412);
    expect((await (await native.get(control)).json()).management_etag).toBe(current.management_etag);
    await page.getByLabel('Try model ID').fill('claude-sonnet-4');
    await page.getByRole('button', { name: 'Check saved routes', exact: true }).click();
    await expect(page.locator('.route-try-result')).toContainText('claude-sonnet-*');
    const execution = await native.post(`${origin}/workspaces/priority-proof/v1/responses`, { data: { model: 'claude-sonnet-4', input: 'priority proof' } });
    expect(execution.status()).toBeGreaterThanOrEqual(400);
    expect(models.length).toBeGreaterThan(0);
    expect(models.every(model => model === 'fixed-glob')).toBe(true);
    const catalog = await (await native.get(`${origin}/workspaces/priority-proof/v1/models`)).json();
    expect(catalog.data.map((model: { id: string }) => model.id)).toEqual(['coding']);
    const encoded = `${control}/model-routes/${encodeURIComponent('deepseek/*')}`;
    const routeRead = await native.get(encoded);
    expect(routeRead.status()).toBe(200);
    const draft = await routeRead.json();
    const unchanged = await native.put(encoded, { headers: { 'If-Match': `"${current.management_etag}"` }, data: draft });
    expect(unchanged.status()).toBe(200);
    const unchangedWorkspace = await unchanged.json();
    expect(unchangedWorkspace.management_etag).toBe(current.management_etag);
    expect(unchangedWorkspace).not.toHaveProperty('replayed');
    const stalePut = await native.put(encoded, { headers: { 'If-Match': `"${saved.management_etag}"` }, data: draft });
    expect(stalePut.status()).toBe(412);
    expect((await (await native.get(control)).json()).management_etag).toBe(current.management_etag);
    await page.getByRole('link', { name: 'deepseek/*', exact: true }).click();
    await expect(page).toHaveURL(/deepseek%2F\*$/);
    await expect(page.getByRole('heading', { name: 'Route · deepseek/*', exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Route · deepseek/*', exact: true })).toBeVisible();
    for (const width of [1440, 390, 759, 760]) {
      await page.setViewportSize({ width, height: 1000 });
      await expect(page.locator('.priority-route')).toHaveCount(4);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: testInfo.outputPath(`priority-${width}.png`), fullPage: true });
    }
    const deleted = await native.delete(encoded, { headers: { 'If-Match': `"${current.management_etag}"` } });
    expect(deleted.status()).toBe(200);
    expect((await native.get(encoded)).status()).toBe(404);
  } finally {
    await native.dispose();
    await new Promise<void>((resolve, reject) => upstream.close(error => error ? reject(error) : resolve()));
  }
});

for (const scenario of ['duplicate pattern', 'stale priority'] as const) {
  test(`${scenario} cannot silently overwrite saved routing`, async ({ page }) => {
    const reply = (body: unknown, status = 200) => ({ status, contentType: status === 200 ? 'application/json' : 'application/problem+json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) });
    const routes = ['coding', '*'].map((name, i) => ({ name, tiers: [{ targets: [{ id: `target-${i}`, model: 'fixed', protocol: 'responses', connection_id: 'cn_model' }] }] }));
    await page.route('**/api/v1/node', route => route.fulfill(reply({ configuration_mode: 'managed' })));
    await page.route('**/api/v1/connections', route => route.fulfill(reply([{ id: 'cn_model', display_name: 'Capacity', kind: 'model' }])));
    await page.route('**/api/v1/workspaces/priority-stale', route => route.fulfill(reply({ slug: 'priority-stale', display_name: '', management_etag: 'g1-1', management_revision: 1, routes, enabled_services: ['models'], readiness: { llm: 'ready' }, mcp_mounts: [] })));
    await page.route('**/api/v1/workspaces/priority-stale/model-routes', route => route.fulfill(reply({ type: 'about:blank', title: 'Workspace changed', detail: 'Inspect current routing.', status: 412, code: 'stale_revision' }, 412)));
    await page.goto('/workspaces/priority-stale/services/models');
    if (scenario === 'duplicate pattern') {
      await page.getByRole('button', { name: 'New route', exact: true }).click();
      const dialog = page.getByRole('dialog');
      await dialog.getByLabel('Model pattern').fill('coding');
      await dialog.getByRole('combobox', { name: 'Connection', exact: true }).selectOption('cn_model');
      await dialog.getByLabel('Upstream model').fill('different');
      await expect(dialog.getByRole('button', { name: 'Create route', exact: true })).toBeDisabled();
    } else {
      await page.getByRole('button', { name: 'Move coding down', exact: true }).click();
      await page.getByRole('button', { name: 'Apply priority', exact: true }).click();
      await expect(page.getByRole('alert')).toContainText('Configuration changed');
      await expect(page.getByRole('button', { name: 'Apply priority', exact: true })).toBeDisabled();
    }
  });
}
