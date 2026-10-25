import { test, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';

// All configuration is authored through the embedded Operator. Only the model
// upstream is synthetic; transport, Node publication, routing and Activity are real.
test('Web Operator creates reusable capacity and a first route, then shows actual execution', async ({ page, playwright }, info) => {
  const requests: { path?: string; authorization?: string; body: string }[] = [];
  const upstream = createServer(async (request, response) => {
    let body = '';
    for await (const chunk of request) body += String(chunk);
    requests.push({ path: request.url, authorization: request.headers.authorization, body });
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ id: 'resp_browser', object: 'response', created_at: 1, status: 'completed', model: 'browser-model',
      output: [{ type: 'message', id: 'msg_browser', role: 'assistant', status: 'completed', content: [{ type: 'output_text', text: 'WEB-FIRST-OK', annotations: [] }] }],
      usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 } }));
  });
  await new Promise<void>(resolve => upstream.listen(0, '127.0.0.1', resolve));
  const address = upstream.address();
  if (!address || typeof address === 'string') throw new Error('synthetic upstream did not bind');
  const origin = 'http://127.0.0.1:4179';
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  try {
    const launch = await native.post(`${origin}/api/v1/browser-launches`);
    expect(launch.status()).toBe(201);
    const { launch_path: path } = await launch.json() as { launch_path: string };
    await page.goto(origin + path);
    await expect(page).toHaveURL(`${origin}/workspaces`);
    await page.getByRole('button', { name: 'New Workspace' }).click();
    let dialog = page.getByRole('dialog');
    await dialog.getByLabel('Workspace address (permanent)').fill('browser-first');
    await dialog.getByLabel('Display name').fill('Browser first route');
    await dialog.getByRole('button', { name: 'Create Workspace' }).click();
    await expect(page).toHaveURL(/\/workspaces\/browser-first\/overview$/);
    const empty = await (await native.get(`${origin}/api/v1/workspaces/browser-first`)).json();
    expect(empty.routes).toEqual([]);
    expect(empty.enabled_services).toEqual([]);
    await page.goto(`${origin}/workspaces/browser-first/services`);
    await page.locator('.servicecontrol').filter({ has: page.getByText('Models', { exact: true }) }).getByRole('button', { name: 'Enable', exact: true }).click();
    await expect(page.locator('.servicecontrol').filter({ has: page.getByText('Models', { exact: true }) }).getByRole('button', { name: 'Disable', exact: true })).toBeVisible();
    await page.goto(`${origin}/workspaces/browser-first/services/models`);
    await page.getByRole('button', { name: 'New route' }).click();
    dialog = page.getByRole('dialog', { name: 'Create Model route' });
    await dialog.getByLabel('Model pattern').fill('coding');
    await dialog.getByRole('button', { name: 'New Model Connection' }).click();
    const connectionDialog = page.getByRole('dialog', { name: 'New Connection' });
    await expect(connectionDialog.getByLabel('Kind')).toBeDisabled();
    await connectionDialog.getByLabel('Name', { exact: true }).fill('Browser model capacity');
    await connectionDialog.getByLabel('Provider (standard API)').fill('lmstudio');
    await connectionDialog.getByLabel('Override provider endpoint (optional)').fill(`http://127.0.0.1:${address.port}/v1`);
    await connectionDialog.getByLabel('Credential reference', { exact: true }).fill('env:SWOBU_WEB_PROOF_KEY');
    await connectionDialog.getByRole('button', { name: 'Create Connection' }).click();
    await expect(connectionDialog).toHaveCount(0);
    await expect(dialog.getByLabel('Model pattern')).toHaveValue('coding');
    const connectionID = await dialog.getByRole('combobox', { name: 'Connection', exact: true }).inputValue();
    expect(connectionID).toMatch(/^cn_/);
    const unsaved = await (await native.get(`${origin}/api/v1/workspaces/browser-first`)).json();
    expect(unsaved.routes).toEqual([]);
    await dialog.getByLabel('Upstream model').fill('browser-model');
    await dialog.getByRole('button', { name: 'Create route' }).click();
    await expect(dialog).toHaveCount(0);
    await page.reload();
    await expect(page.getByRole('link', { name: 'coding', exact: true })).toBeVisible();
    const result = await native.post(`${origin}/workspaces/browser-first/v1/responses`, { data: { model: 'coding', input: 'browser proof', stream: false } });
    expect(result.status()).toBe(200);
    expect(await result.text()).toContain('WEB-FIRST-OK');
    expect(requests).toHaveLength(1);
    expect(requests[0].path).toBe('/v1/responses');
    expect(requests[0].authorization).toBe('Bearer synthetic-browser-proof');
    expect(JSON.parse(requests[0].body).model).toBe('browser-model');
    await page.goto(`${origin}/activity`);
    await expect(page.getByText('browser-first · coding', { exact: true })).toBeVisible();
    await expect(page.getByText('browser-model', { exact: false }).first()).toBeVisible();
    const committed = await (await native.get(`${origin}/api/v1/workspaces/browser-first`)).json();
    expect(committed.routes[0].tiers[0].targets[0].connection_id).toBe(connectionID);
    await expect.poll(async () => {
      const retained = await native.get(`${origin}/api/v1/activity?kind=exchange&limit=50&workspace=browser-first&principal=local&result=succeeded`);
      return (await retained.json()).exchanges.length;
    }).toBe(1);
    await page.goto(`${origin}/activity?workspace=browser-first&kind=exchange&principal=local&result=succeeded`);
    await expect(page.getByRole('link', { name: /browser-first · coding/ })).toBeVisible();
    const ndjson = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download first page' }).click();
    const download = await ndjson;
    const exported = await readFile((await download.path())!, 'utf8');
    const record = JSON.parse(exported.trim());
    expect(record.kind).toBe('exchange'); expect(record.exchange.workspace).toBe('browser-first');
    expect(record.exchange.access_id).toBe('local'); expect(record.exchange.state).toBe('succeeded');
    expect(exported).not.toContain('WEB-FIRST-OK'); expect(exported).not.toContain('synthetic-browser-proof');
    await page.goto(`${origin}/settings/history?workspace=browser-first&kind=exchange&principal=local&result=succeeded`);
    await page.getByRole('combobox', { name: 'File format' }).selectOption('csv');
    const csv = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download first page' }).click();
    const csvFile = await csv;
    expect(csvFile.suggestedFilename()).toBe('swobu-activity-exchange-browser-first.csv');
    const csvText = await readFile((await csvFile.path())!, 'utf8');
    expect(csvText).toContain(record.exchange.exchange_id); expect(csvText).not.toContain('WEB-FIRST-OK');
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 950 });
      await page.getByRole('heading', { name: 'Settings', exact: true }).click();
      await page.evaluate(() => {
        if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
        window.scrollTo(0, 0);
      });
      await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath(`real-history-${width}.png`), fullPage: true });
    }
    await page.goto(`${origin}/workspaces/browser-first/limits`);
    await page.getByRole('combobox', { name: 'Requests / minute policy' }).selectOption('limited');
    await page.getByRole('spinbutton', { name: 'Requests / minute', exact: true }).fill('1');
    await page.getByRole('combobox', { name: 'Concurrent exchanges policy' }).selectOption('limited');
    await page.getByRole('spinbutton', { name: 'Concurrent exchanges', exact: true }).fill('2');
    await page.getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect(page.getByRole('status')).toHaveText('Workspace limits saved.');
    await page.reload();
    await expect(page.getByRole('spinbutton', { name: 'Requests / minute', exact: true })).toHaveValue('1');
    const savedPolicy = await native.get(`${origin}/api/v1/workspaces/browser-first/service-limits`);
    expect(savedPolicy.status()).toBe(200);
    expect((await savedPolicy.json()).models).toEqual({ requests_per_minute: 1, concurrent: 2 });
    await expect(page.getByText('No spending ceiling')).toBeVisible();
    const admitted = await native.post(`${origin}/workspaces/browser-first/v1/responses`, { data: { model: 'coding', input: 'limited browser proof', stream: false } });
    expect(admitted.status()).toBe(200);
    const denied = await native.post(`${origin}/workspaces/browser-first/v1/responses`, { data: { model: 'coding', input: 'exhausted', stream: false } });
    expect(denied.status()).toBe(429); expect(Number(denied.headers()['retry-after'])).toBeGreaterThan(0);
    expect(requests).toHaveLength(2);
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 950 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath(`real-limits-${width}.png`), fullPage: true });
    }
  } finally {
    await native.dispose();
    await new Promise<void>((resolve, reject) => upstream.close(error => error ? reject(error) : resolve()));
  }
});
