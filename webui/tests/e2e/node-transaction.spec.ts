import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

for (const width of [390, 1440]) {
test(`embedded Operator reviews builtin Node authority at ${width}px`, async ({ page, playwright }, info) => {
  await page.setViewportSize({ width, height: 950 });
  const origin = 'http://127.0.0.1:4179';
  const slug = `builtin-admin-${width}`;
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  try {
    const created = await native.post(`${origin}/api/v1/workspaces`, {
      headers: { 'Idempotency-Key': `create-${slug}` }, data: { slug },
    });
    expect(created.status()).toBe(201);
    const before = await native.get(`${origin}/api/v1/workspaces/${slug}`);
    expect((await native.put(`${origin}/api/v1/workspaces/${slug}/services/mcp`, {
      headers: { 'If-Match': before.headers().etag },
    })).status()).toBe(200);
    const launch = await native.post(`${origin}/api/v1/browser-launches`);
    expect(launch.status()).toBe(201);
    await page.goto(origin + (await launch.json()).launch_path);
    await page.goto(`${origin}/workspaces/${slug}/services/mcp`);
    await page.getByRole('button', { name: 'Add server' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Namespace').fill('node');
    await dialog.getByLabel('MCP source').selectOption('builtin:swobu-admin');
    await dialog.getByLabel('Selected Admin tool names').fill('workspaces_list\nworkspace_get');
    await expect(dialog).toContainText('including broad Workspace grants');
    await dialog.getByRole('button', { name: 'Review source & tools' }).click();
    await expect(dialog.getByRole('group', { name: 'MCP authority impact review' })).toContainText('Node');
    const previewed = await (await native.get(`${origin}/api/v1/workspaces/${slug}`)).json();
    expect(previewed.mcp_mounts ?? []).toEqual([]);
    await expect(dialog.getByRole('button', { name: 'Apply reviewed mount' })).toBeEnabled();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await dialog.screenshot({ path: info.outputPath('builtin-authority-review.png') });
    await dialog.getByRole('button', { name: 'Apply reviewed mount' }).click();
    await expect(dialog).toHaveCount(0);
    await page.reload();
    await expect(page.getByText('Swobu Node administration · builtin', { exact: true })).toBeVisible();
    const saved = await (await native.get(`${origin}/api/v1/workspaces/${slug}`)).json();
    expect(saved.mcp_mounts).toEqual([expect.objectContaining({ alias: 'node', builtin: 'swobu-admin', allowed_tools: ['workspace_get', 'workspaces_list'] })]);
    for (const service of ['models', 'search']) {
      const current = await native.get(`${origin}/api/v1/workspaces/${slug}`);
      expect((await native.put(`${origin}/api/v1/workspaces/${slug}/services/${service}`, {
        headers: { 'If-Match': current.headers().etag },
      })).status()).toBe(200);
    }
    await page.goto(`${origin}/workspaces/${slug}/overview`);
    await expect(page.locator('.servicerow')).toHaveCount(3);
    await expect(page.getByText('1 MCP source', { exact: true })).toBeVisible();
    await expect(page.getByText('0 routes · 0 targets', { exact: true })).toBeVisible();
    await expect(page.getByText('No Search source', { exact: true })).toBeVisible();
    await expect(page.locator('.workspace-policy-metric').first().locator('strong')).toHaveText('0');
    await expect(page.locator('.workspace-reachability')).toContainText('Local only');
    await expect(page.locator('.workspace-recent')).toContainText('No operations in the current snapshot');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('builtin-overview.png'), fullPage: true });
    await page.goto(`${origin}/workspaces/${slug}/connect`);
    await expect(page.getByText('If swobu-admin is mounted', { exact: false })).toBeVisible();
    await expect(page.getByText('Node administration remains local', { exact: false })).toHaveCount(0);
  } finally { await native.dispose(); }
});
}

// Only the upstream is synthetic. Browser consent, owner revisions, Node writes
// and Workspace MCP execution use the production embedded Operator and daemon.
test('embedded Operator creates and edits a reviewed MCP mount on the real Node', async ({ page, playwright }) => {
  const upstream = `
    require('node:readline').createInterface({ input: process.stdin }).on('line', raw => {
      const message = JSON.parse(raw);
      if (message.id === undefined) return;
      const result = message.method === 'initialize' ? {
        protocolVersion: message.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'node-proof', version: '1' },
      } : message.method === 'tools/list' ? { tools: ['read', 'write'].map(name => ({ name, inputSchema: { type: 'object' } })) }
        : { content: [{ type: 'text', text: 'NODE-MOUNT-OK' }] };
      process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: message.id, result }) + '\\n');
    });`;
  const origin = 'http://127.0.0.1:4179';
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  try {
    const created = await native.post(`${origin}/api/v1/workspaces`, {
      headers: { 'Idempotency-Key': 'node-mount-workspace' }, data: { slug: 'node-mount', display_name: 'Node mount proof' },
    });
    expect(created.status()).toBe(201);
    const before = await native.get(`${origin}/api/v1/workspaces/node-mount`);
    const enabled = await native.put(`${origin}/api/v1/workspaces/node-mount/services/mcp`, { headers: { 'If-Match': before.headers().etag } });
    expect(enabled.status()).toBe(200);
    const launch = await native.post(`${origin}/api/v1/browser-launches`);
    expect(launch.status()).toBe(201);
    const { launch_path: path } = await launch.json();
    await page.goto(origin + path);
    await expect(page).toHaveURL(`${origin}/workspaces`);
    // Stdio uses the supported typed API; HTTP sources require guarded HTTPS,
    // so a loopback HTTP server cannot stand in for an admitted upstream.
    const connection = await native.post(`${origin}/api/v1/connections`, {
      headers: { 'Idempotency-Key': 'node-mount-connection' }, data: {
        kind: 'mcp', display_name: 'Node MCP upstream', spec: { transport: 'stdio',
          execution_identity: 'Proof account', permission_summary: 'Read and write proof documents',
          configuration: { executable: process.execPath, args: ['-e', upstream], env_refs: {} } },
      },
    });
    expect(connection.status()).toBe(201);
    const { connection: { id } } = await connection.json();
    await page.goto(`${origin}/workspaces/node-mount/services/mcp`);
    await page.getByRole('button', { name: 'Add server' }).click();
    let dialog = page.getByRole('dialog');
    await dialog.getByLabel('Namespace').fill('docs');
    await dialog.getByLabel('MCP source').selectOption(id);
    await dialog.getByRole('checkbox', { name: 'read', exact: true }).check();
    await dialog.getByRole('button', { name: 'Review source & tools' }).click();
    await expect(dialog.getByRole('group', { name: 'MCP authority impact review' })).toContainText('Proof account');
    const previewed = await (await native.get(`${origin}/api/v1/workspaces/node-mount`)).json();
    expect(previewed.mcp_mounts ?? []).toEqual([]);
    expect(`"${previewed.management_etag}"`).toBe(enabled.headers().etag);
    const applyRequest = page.waitForRequest(request => request.method() === 'PUT' && request.url().includes('/mcp-mounts/docs?dry_run=false'));
    await dialog.getByRole('button', { name: 'Apply reviewed mount' }).click();
    const sent = await applyRequest;
    expect(sent.headers()['idempotency-key']).toBeUndefined();
    await expect(dialog).toHaveCount(0);
    await page.reload();
    await expect(page.getByText('docs.read', { exact: true })).toBeVisible();
    // The exact old review is stale even though the desired mount already exists.
    const stale = await native.put(sent.url(), { headers: { 'If-Match': sent.headers()['if-match'] }, data: sent.postDataJSON() });
    expect(stale.status()).toBe(412);
    await page.getByRole('button', { name: 'Edit tools' }).click();
    dialog = page.getByRole('dialog');
    await dialog.getByRole('checkbox', { name: 'write', exact: true }).check();
    await dialog.getByRole('button', { name: 'Review source & tools' }).click();
    await expect(dialog.getByRole('group', { name: 'MCP authority impact review' })).toBeVisible();
    await dialog.getByRole('button', { name: 'Apply reviewed mount' }).click();
    await expect(dialog).toHaveCount(0);
    await page.reload();
    await expect(page.getByText('docs.write', { exact: true })).toBeVisible();
    const tools = await native.post(`${origin}/workspaces/node-mount/mcp`, { data: { jsonrpc: '2.0', id: 1, method: 'tools/list' } });
    expect(tools.status()).toBe(200);
    const wire = await tools.text();
    // Workspace MCP permits JSON or SSE; assert the returned result in either
    // negotiated transport instead of treating an SSE frame as a JSON body.
    const catalog = JSON.parse(tools.headers()['content-type']?.startsWith('text/event-stream')
      ? wire.split('\n').find(line => line.startsWith('data:'))!.slice(5).trim() : wire);
    expect(catalog.result.tools.map((tool: { name: string }) => tool.name).sort()).toEqual(['docs__read', 'docs__write']);
    const executed = await native.post(`${origin}/workspaces/node-mount/mcp`, { data: { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'docs__read', arguments: {} } } });
    expect(executed.status()).toBe(200);
    expect(await executed.text()).toContain('NODE-MOUNT-OK');
    // The real Workspace call must settle through the writer before its
    // content-free retained evidence can be downloaded by the browser.
    await expect.poll(async () => {
      const retained = await native.get(`${origin}/api/v1/activity?workspace=node-mount&kind=mcp_operation&limit=50`);
      return (await retained.json()).mcp_operations?.length ?? 0;
    }).toBe(1);
    await page.goto(`${origin}/settings/history?workspace=node-mount&kind=mcp_operation`);
    const ndjson = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download first page' }).click();
    const file = await ndjson;
    expect(file.suggestedFilename()).toBe('swobu-activity-mcp_operation-node-mount.ndjson');
    const text = await readFile((await file.path())!, 'utf8');
    const record = JSON.parse(text.trim());
    expect(record.kind).toBe('mcp_operation');
    expect(record.exchange).toBeUndefined();
    expect(record.mcp_operation).toMatchObject({ workspace: 'node-mount', access_id: 'local', tool: 'read', outcome: 'succeeded' });
    expect(text).not.toContain('NODE-MOUNT-OK');
  } finally {
    await native.dispose();
  }
});
