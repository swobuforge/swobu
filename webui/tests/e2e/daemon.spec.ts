import { test, expect } from '@playwright/test';

// Real daemon admission proof is separate from the staged shell reproduction.
// W01 owns serving the new bundle; this journey uses today's embedded document.
test('real daemon bootstrap consumes launch once and admits browser control', async ({ page, playwright }) => {
  const origin = 'http://127.0.0.1:4179';
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  try {
    const denied = await page.goto(`${origin}/workspaces`);
    expect(denied?.status()).toBe(401);
    const launch = await native.post(`${origin}/api/v1/browser-launches`);
    expect(launch.status()).toBe(201);
    const { launch_path: path } = await launch.json() as { launch_path: string };
    const token = path.split('#')[1];
    await page.goto(origin + path);
    await expect(page).toHaveURL(`${origin}/workspaces`);
    expect(await page.evaluate(() => location.hash)).toBe('');
    const cookie = (await page.context().cookies()).find(value => value.name === 'swobu_operator_session');
    expect(cookie?.httpOnly).toBe(true);
    expect(cookie?.sameSite).toBe('Strict');
    const status = await page.evaluate(async () => (await fetch('/api/v1/status', { credentials: 'same-origin' })).status);
    expect(status).toBe(200);
    const replay = await page.evaluate(async token => (await fetch('/api/v1/browser-session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) })).status, token);
    expect(replay).toBe(401);
  } finally { await native.dispose(); }
});

test('embedded Operator reviews and persists Principal request ceilings on the real Node', async ({ page, playwright }) => {
  const origin = 'http://127.0.0.1:4179';
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  try {
    const created = await native.post(`${origin}/api/v1/workspaces`, { headers: { 'Idempotency-Key': 's06-browser-workspace' }, data: { slug: 'rate-proof', display_name: 'Rate proof' } });
    expect(created.status()).toBe(201);
    const workspace = await native.get(`${origin}/api/v1/workspaces/rate-proof`);
    const enabled = await native.put(`${origin}/api/v1/workspaces/rate-proof/services/models`, { headers: { 'If-Match': workspace.headers()['etag'] } });
    expect(enabled.status()).toBe(200);
    const createdPrincipal = await native.post(`${origin}/api/v1/principals`, { headers: { 'Idempotency-Key': 's06-browser-principal' }, data: { label: 'Rate proof Alice' } });
    expect(createdPrincipal.status()).toBe(201);
    const principal = await createdPrincipal.json() as { id: string };
    const launch = await native.post(`${origin}/api/v1/browser-launches`);
    expect(launch.status()).toBe(201);
    const { launch_path: path } = await launch.json() as { launch_path: string };
    await page.goto(origin + path);
    await expect(page).toHaveURL(`${origin}/workspaces`);
    await page.goto(`${origin}/workspaces/rate-proof/access`);
    await page.getByRole('button', { name: 'Grant access', exact: true }).click();
    await page.getByRole('button', { name: /Rate proof Alice/ }).click();
    await page.getByRole('spinbutton', { name: 'Models requests / minute' }).fill('7');
    await page.getByRole('button', { name: 'Review grant' }).click();
    await expect(page.getByRole('dialog')).toContainText('Models: 7 req/min');
    await page.getByRole('button', { name: 'Confirm grant' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await page.reload();
    await page.getByRole('button', { name: 'Edit Services' }).click();
    await expect(page.getByRole('spinbutton', { name: 'Models requests / minute' })).toHaveValue('7');
    const state = await native.get(`${origin}/api/v1/principals/${principal.id}`);
    expect((await state.json()).permissions).toEqual([expect.objectContaining({ workspace: 'rate-proof', services: ['models'], request_limits: [{ service: 'models', requests_per_minute: 7 }] })]);
    await page.goto(`${origin}/workspaces/rate-proof/overview`);
    await expect(page.locator('.servicerow')).toHaveCount(1);
    await expect(page.getByText('0 routes · 0 targets', { exact: true })).toBeVisible();
    await expect(page.locator('.workspace-policy-metric').first().locator('strong')).toHaveText('1');
    await expect(page.locator('.workspace-policy-metric').last().locator('strong')).toHaveText('1');
  } finally { await native.dispose(); }
});

test('embedded Search editor saves ordered Connections on the real Node', async ({ page, playwright }, info) => {
  const origin = 'http://127.0.0.1:4179';
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  try {
    expect((await native.post(`${origin}/api/v1/workspaces`, { headers: { 'Idempotency-Key': 'search-browser-workspace' }, data: { slug: 'search-proof', display_name: 'Search proof' } })).status()).toBe(201);
    const workspace = await native.get(`${origin}/api/v1/workspaces/search-proof`);
    expect((await native.put(`${origin}/api/v1/workspaces/search-proof/services/search`, { headers: { 'If-Match': workspace.headers()['etag'] } })).status()).toBe(200);
    const ids: string[] = [];
    for (const name of ['Primary Search', 'Secondary Search', 'Last Search']) {
      const created = await native.post(`${origin}/api/v1/connections`, { headers: { 'Idempotency-Key': `search-browser-${ids.length}` }, data: { display_name: name, kind: 'search', spec: { provider_id: 'fixture', endpoint: 'https://search.example.test/search' } } });
      expect(created.status()).toBe(201); ids.push((await created.json()).connection.id);
    }
    const launch = await native.post(`${origin}/api/v1/browser-launches`);
    expect(launch.status()).toBe(201);
    await page.goto(origin + (await launch.json()).launch_path);
    await expect(page).toHaveURL(`${origin}/workspaces`);
    await page.goto(`${origin}/workspaces/search-proof/services/search`);
    await page.getByLabel('Primary Search Connection').selectOption(ids[0]);
    await page.getByLabel('Add fallback Search Connection').selectOption(ids[1]);
    await page.getByLabel('Add fallback Search Connection').selectOption(ids[2]);
    await page.getByRole('button', { name: `Move ${ids[2]} earlier` }).click();
    const committed = page.waitForResponse(response => response.url().endsWith('/workspaces/search-proof/search-route') && response.request().method() === 'PUT');
    await page.getByRole('button', { name: 'Save Search route' }).click();
    expect((await committed).status()).toBe(200);
    await expect(page.getByRole('button', { name: 'Save Search route' })).toBeDisabled();
    const saved = await native.get(`${origin}/api/v1/workspaces/search-proof/search-route`);
    expect(await saved.json()).toEqual({ connection_id: ids[0], fallback_connection_ids: [ids[2], ids[1]] });
    await page.reload();
    await expect(page.locator('.search-fallback-row').first()).toContainText('Last Search');
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 930 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath(`embedded-search-${width}.png`), fullPage: true });
    }
    await page.getByRole('button', { name: 'Clear route' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Clear route' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect((await native.get(`${origin}/api/v1/workspaces/search-proof/search-route`)).status()).toBe(404);
  } finally { await native.dispose(); }
});

for (const width of [390, 1440]) {
  test(`embedded Operator confirms Service expansion against real grants at ${width}px`, async ({ page, playwright }, info) => {
    await page.setViewportSize({ width, height: 950 });
    const origin = 'http://127.0.0.1:4179';
    const slug = `activation-${width}`;
    const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
    try {
      expect((await native.post(`${origin}/api/v1/workspaces`, {
        headers: { 'Idempotency-Key': slug }, data: { slug },
      })).status()).toBe(201);
      const created = await native.post(`${origin}/api/v1/principals`, {
        headers: { 'Idempotency-Key': slug }, data: { label: `Activation agent ${width}` },
      });
      expect(created.status()).toBe(201);
      const principal = await created.json();
      const grantURL = `${origin}/api/v1/workspaces/${slug}/permissions/${principal.id}`;
      const preview = await native.put(`${grantURL}?dry_run=true`, { data: { action: 'use' } });
      expect(preview.status()).toBe(200);
      const grant = await preview.json();
      expect((await native.put(grantURL, {
        headers: { 'If-Match': `"${grant.revision}"`, 'Idempotency-Key': `grant-${slug}` },
        data: { action: 'use', request_limits: [] },
      })).status()).toBe(200);
      const before = await native.get(`${origin}/api/v1/workspaces/${slug}`);
      expect((await native.put(`${origin}/api/v1/workspaces/${slug}/services/mcp`, {
        headers: { 'If-Match': before.headers().etag },
      })).status()).toBe(428);
      const launch = await native.post(`${origin}/api/v1/browser-launches`);
      expect(launch.status()).toBe(201);
      await page.goto(origin + (await launch.json()).launch_path);
      await page.goto(`${origin}/workspaces/${slug}/services`);
      const row = page.locator('.servicecontrol').filter({ has: page.getByText('MCP', { exact: true }) });
      await row.getByRole('button', { name: 'Enable' }).click();
      const dialog = page.getByRole('dialog', { name: 'Enable MCP' });
      await expect(dialog).toContainText(`Activation agent ${width}`);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath('service-activation.png'), fullPage: true });
      await dialog.getByRole('button', { name: 'Cancel' }).click();
      expect((await (await native.get(`${origin}/api/v1/workspaces/${slug}`)).json()).enabled_services ?? []).toEqual([]);
      await row.getByRole('button', { name: 'Enable' }).click();
      await dialog.getByRole('button', { name: 'Confirm enabling MCP' }).click();
      await expect(dialog).toHaveCount(0);
      await page.reload();
      await expect(row.getByRole('button', { name: 'Disable' })).toBeVisible();
      expect((await (await native.get(`${origin}/api/v1/workspaces/${slug}`)).json()).enabled_services).toEqual(['mcp']);
    } finally { await native.dispose(); }
  });
}
