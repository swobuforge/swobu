import { expect, test, type Page } from '@playwright/test';

function reply(body: unknown, status = 200) {
  return { status, contentType: status >= 400 ? 'application/problem+json' : 'application/json',
    headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) };
}
function workspace() {
  return { slug: 'team', display_name: 'Team', management_etag: 'ws-1', management_revision: 1,
    routes: [], mcp_mounts: [], enabled_services: ['models', 'mcp', 'search'], readiness: { llm: 'not_configured', mcp: 'not_configured' } };
}
async function setup(page: Page) {
  const connections: Array<{ id: string; display_name: string; kind: string; revision: string; spec?: object }> = [];
  const writes: unknown[] = [];
  const intents: string[] = [];
  await page.route('**/api/v1/node', route => route.fulfill(reply({ configuration_mode: 'managed', workspace_count: 1 })));
  await page.route('**/api/v1/workspaces/team', route => route.fulfill(reply(workspace())));
  await page.route('**/api/v1/workspaces/team/services', route => route.fulfill(reply([
    { kind: 'models', enabled: true, configured: false }, { kind: 'mcp', enabled: true, configured: false },
    { kind: 'search', enabled: true, configured: false },
  ])));
  await page.route('**/api/v1/connections', route => {
    if (route.request().method() === 'GET') return route.fulfill(reply(connections));
    expect(route.request().method()).toBe('POST');
    const body = route.request().postDataJSON();
    const intent = route.request().headers()['idempotency-key'];
    expect(intent).toBeTruthy();
    expect(intents).not.toContain(intent);
    intents.push(intent);
    writes.push(body);
    const connection = { id: `cn_added_${intents.length}`, kind: body.kind, display_name: body.display_name, revision: '1',
      spec: body.kind === 'model' ? { shape: 'standard', provider_id: body.spec.provider_id } : body.spec };
    connections.push(connection);
    return route.fulfill(reply({ connection }, 201));
  });
  return { connections, writes, intents };
}

test('a Model route preserves unsaved fields and selects its newly created Connection', async ({ page }) => {
  const state = await setup(page);
  let routeWrites = 0;
  await page.route('**/api/v1/workspaces/team/model-routes/coding', route => {
    routeWrites++;
    expect(route.request().headers()['if-match']).toBe('"ws-1"');
    expect(route.request().postDataJSON().tiers[0].targets[0]).toEqual({
      id: 'primary', connection_id: 'cn_added_1', model: 'gpt-5', protocol: 'responses',
    });
    return route.fulfill(reply(workspace()));
  });
  await page.goto('/workspaces/team/services/models');
  await page.getByRole('button', { name: 'New route' }).click();
  const routeDialog = page.getByRole('dialog', { name: 'Create Model route' });
  await routeDialog.getByLabel('Model pattern').fill('coding');
  await routeDialog.getByLabel('Upstream model').fill('gpt-5');
  await routeDialog.getByRole('button', { name: 'New Model Connection' }).click();
  const connectionDialog = page.getByRole('dialog', { name: 'New Connection' });
  await expect(connectionDialog.getByLabel('Kind')).toBeDisabled();
  await expect(connectionDialog.getByLabel('Kind')).toHaveValue('model');
  await connectionDialog.getByLabel('Name', { exact: true }).fill('Capacity');
  await connectionDialog.getByLabel('Credential reference', { exact: true }).fill('env:MODEL_TOKEN');
  await connectionDialog.getByRole('button', { name: 'Create Connection' }).click();
  await expect(connectionDialog).toHaveCount(0);
  await expect(routeDialog.getByLabel('Model pattern')).toHaveValue('coding');
  await expect(routeDialog.getByLabel('Upstream model')).toHaveValue('gpt-5');
  await expect(routeDialog.getByLabel('Connection')).toHaveValue('cn_added_1');
  expect(routeWrites).toBe(0);
  await routeDialog.getByRole('button', { name: 'Create route' }).click();
  await expect(routeDialog).toHaveCount(0);
  expect(routeWrites).toBe(1);
  expect(state.writes).toHaveLength(1);
  expect((state.writes[0] as { kind: string }).kind).toBe('model');
});

test('contextual Connection cancellation leaves the MCP authority review untouched', async ({ page }) => {
  const state = await setup(page);
  await page.route('**/api/v1/workspaces/team/mcp-mounts', route => route.fulfill(reply([])));
  await page.goto('/workspaces/team/services/mcp');
  await page.getByRole('button', { name: 'Add server' }).click();
  const mount = page.getByRole('dialog', { name: 'Add MCP source' });
  await mount.getByLabel('Namespace').fill('github');
  await mount.getByRole('button', { name: 'New MCP Connection' }).click();
  const create = page.getByRole('dialog', { name: 'New Connection' });
  await expect(create.getByLabel('Kind')).toHaveValue('mcp');
  await expect(create.getByLabel('Kind')).toBeDisabled();
  await create.getByRole('button', { name: 'Cancel' }).click();
  await expect(create).toHaveCount(0);
  await expect(mount.getByLabel('Namespace')).toHaveValue('github');
  await expect(mount.getByLabel('MCP source')).toHaveValue('');
  expect(state.writes).toHaveLength(0);
});

test('created MCP Connection discovers tools but still requires authority review', async ({ page }) => {
  const state = await setup(page);
  await page.route('**/api/v1/workspaces/team/mcp-mounts', route => route.fulfill(reply([])));
  await page.route('**/api/v1/connections/cn_added_1/mcp-catalog', route => route.fulfill(reply([{ name: 'lookup', description: 'Read docs' }])));
  await page.goto('/workspaces/team/services/mcp');
  await page.getByRole('button', { name: 'Add server' }).click();
  const mount = page.getByRole('dialog', { name: 'Add MCP source' });
  await mount.getByLabel('Namespace').fill('docs');
  await mount.getByRole('button', { name: 'New MCP Connection' }).click();
  const create = page.getByRole('dialog', { name: 'New Connection' });
  await create.getByLabel('Name', { exact: true }).fill('Docs MCP');
  await create.getByLabel('Endpoint').fill('https://mcp.example.test/mcp');
  await create.getByRole('button', { name: 'Create Connection' }).click();
  await expect(create).toHaveCount(0);
  await expect(mount.getByLabel('Namespace')).toHaveValue('docs');
  await expect(mount.getByLabel('MCP source')).toHaveValue('cn_added_1');
  await mount.getByRole('checkbox', { name: 'lookup' }).check();
  await expect(mount.getByRole('button', { name: 'Review source & tools' })).toBeEnabled();
  expect(state.writes).toHaveLength(1);
  expect((state.writes[0] as { kind: string }).kind).toBe('mcp');
});

test('Search creates primary and fallback Connections without implicitly saving the route', async ({ page }) => {
  const state = await setup(page);
  let saves = 0;
  await page.route('**/api/v1/workspaces/team/search-route', route => {
    if (route.request().method() === 'GET') return route.fulfill(reply({ title: 'Not found', status: 404 }, 404));
    saves++;
    expect(route.request().headers()['if-match']).toBe('"ws-1"');
    expect(route.request().postDataJSON()).toEqual({ connection_id: 'cn_added_1', fallback_connection_ids: ['cn_added_2'] });
    return route.fulfill(reply(workspace()));
  });
  await page.goto('/workspaces/team/services/search');
  await page.getByRole('button', { name: 'New primary Connection' }).click();
  let dialog = page.getByRole('dialog', { name: 'New Connection' });
  await expect(dialog.getByLabel('Kind')).toHaveValue('search');
  await expect(dialog.getByLabel('Kind')).toBeDisabled();
  await dialog.getByLabel('Name', { exact: true }).fill('Primary');
  await dialog.getByLabel('Endpoint').fill('https://search.example.test/search');
  await dialog.getByRole('button', { name: 'Create Connection' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByLabel('Primary Search Connection')).toHaveValue('cn_added_1');
  await page.getByRole('button', { name: 'New fallback Connection' }).click();
  dialog = page.getByRole('dialog', { name: 'New Connection' });
  await dialog.getByLabel('Name', { exact: true }).fill('Fallback');
  await dialog.getByLabel('Endpoint').fill('https://backup.example.test/search');
  await dialog.getByRole('button', { name: 'Create Connection' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.search-fallback-row')).toHaveCount(1);
  expect(saves).toBe(0);
  await page.getByRole('button', { name: 'Save Search route' }).click();
  await expect.poll(() => saves).toBe(1);
  expect(state.writes.map(item => (item as { kind: string }).kind)).toEqual(['search', 'search']);
  expect(new Set(state.intents).size).toBe(2);
});

for (const width of [390, 1440]) {
  test(`nested creation preserves retry material and keyboard focus at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await setup(page);
    const attempts: Array<{ body: unknown; key: string }> = [];
    await page.route('**/api/v1/connections', route => {
      if (route.request().method() === 'GET') return route.fulfill(reply([]));
      attempts.push({ body: route.request().postDataJSON(), key: route.request().headers()['idempotency-key'] });
      if (attempts.length === 1) return width === 1440 ? route.abort('connectionreset') : route.fulfill(reply({ title: 'Outcome unknown', status: 503 }, 503));
      return route.fulfill(reply({ connection: { id: 'cn_recovered', display_name: 'Retained', kind: 'model', revision: '1' } }, 201));
    });
    await page.goto('/workspaces/team/services/models');
    await page.getByRole('button', { name: 'New route' }).click();
    const parent = page.getByRole('dialog', { name: 'Create Model route' });
    await parent.getByLabel('Model pattern').fill('retained');
    const opener = parent.getByRole('button', { name: 'New Model Connection' });
    await opener.click();
    const child = page.getByRole('dialog', { name: 'New Connection' });
    await expect(child.getByLabel('Name', { exact: true })).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(child).toHaveCount(0);
    await expect(opener).toBeFocused();
    await opener.click();
    await child.getByLabel('Name', { exact: true }).fill('Retained');
    await child.getByLabel('Credential reference', { exact: true }).fill('env:TOKEN');
    await child.getByRole('button', { name: 'Create Connection' }).click();
    await expect(child.getByRole('alert')).toBeVisible();
    await expect(child.getByLabel('Name', { exact: true })).toBeDisabled();
    await expect(child.getByLabel('Credential reference', { exact: true })).toBeDisabled();
    expect(attempts).toHaveLength(1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    expect(await child.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath(`creation-uncertain-${width}.png`), fullPage: true });
    await child.getByRole('button', { name: 'Retry same creation' }).click();
    await expect(child).toHaveCount(0);
    expect(attempts).toHaveLength(2);
    expect(attempts[1]).toEqual(attempts[0]);
    await expect(parent.getByLabel('Model pattern')).toHaveValue('retained');
    await expect(opener).toBeFocused();
  });
}

test('standalone creation still opens its authoritative Connection detail', async ({ page }) => {
  const state = await setup(page);
  await page.route('**/api/v1/connections/cn_added_1', route => route.fulfill(reply(state.connections[0])));
  await page.route('**/api/v1/connections/cn_added_1/references', route => route.fulfill(reply({ consumers: [] })));
  await page.goto('/connections');
  await page.getByRole('button', { name: 'New Connection' }).click();
  const dialog = page.getByRole('dialog', { name: 'New Connection' });
  await expect(dialog.getByLabel('Kind')).toBeEnabled();
  await dialog.getByLabel('Name', { exact: true }).fill('Standalone');
  await dialog.getByLabel('Credential reference', { exact: true }).fill('env:TOKEN');
  await dialog.getByRole('button', { name: 'Create Connection' }).click();
  await expect(page).toHaveURL(/\/connections\/cn_added_1$/);
  await expect(page.getByRole('heading', { name: 'Standalone', exact: true })).toBeVisible();
  expect(state.writes).toHaveLength(1);
});

test('pending creation locks material; a rejected draft can be corrected with a new intent', async ({ page }) => {
  await setup(page);
  const keys: string[] = [];
  let finish: (() => void) | undefined;
  await page.route('**/api/v1/connections', async route => {
    if (route.request().method() === 'GET') return route.fulfill(reply([]));
    keys.push(route.request().headers()['idempotency-key']);
    if (keys.length === 1) {
      await new Promise<void>(resolve => { finish = resolve; });
      return route.fulfill(reply({ type: 'about:blank', code: 'INVALID', title: 'Invalid provider', status: 400 }, 400));
    }
    return route.fulfill(reply({ connection: { id: 'cn_corrected', display_name: 'Corrected', kind: 'model', revision: '1' } }, 201));
  });
  await page.goto('/workspaces/team/services/models');
  await page.getByRole('button', { name: 'New route' }).click();
  const parent = page.getByRole('dialog', { name: 'Create Model route' });
  await parent.getByRole('button', { name: 'New Model Connection' }).click();
  const dialog = page.getByRole('dialog', { name: 'New Connection' });
  await dialog.getByLabel('Name', { exact: true }).fill('Corrected');
  await dialog.getByLabel('Credential reference', { exact: true }).fill('env:TOKEN');
  await dialog.getByRole('button', { name: 'Create Connection' }).click();
  await expect.poll(() => keys.length).toBe(1);
  await expect(dialog.getByLabel('Name', { exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Creating…' })).toBeDisabled();
  finish!();
  await expect(dialog.getByRole('alert')).toContainText('Invalid provider');
  await expect(dialog.getByLabel('Provider (standard API)')).toBeEnabled();
  await dialog.getByLabel('Provider (standard API)').fill('lmstudio');
  await dialog.getByRole('button', { name: 'Create Connection' }).click();
  await expect(dialog).toHaveCount(0);
  expect(keys).toHaveLength(2);
  expect(keys[0]).not.toBe(keys[1]);
});

test('editing a Model route attaches capacity to the chosen fallback target only', async ({ page }) => {
  const state = await setup(page);
  state.connections.push({ id: 'cn_existing', display_name: 'Existing', kind: 'model', revision: '1' });
  const tiers = [{ targets: [{ id: 'primary', connection_id: 'cn_existing', model: 'primary-model', protocol: 'responses' }] },
    { targets: [{ id: 'backup', connection_id: 'cn_existing', model: 'backup-model', protocol: 'responses' }] }];
  const saved = { ...workspace(), routes: [{ name: 'coding', tiers }] };
  await page.route('**/api/v1/workspaces/team', route => route.fulfill(reply(saved)));
  let writes = 0;
  await page.route('**/api/v1/workspaces/team/model-routes/coding', route => {
    if (route.request().method() === 'GET') return route.fulfill(reply({ tiers }));
    writes++;
    expect(route.request().postDataJSON().tiers).toEqual([tiers[0], { targets: [{ ...tiers[1].targets[0], connection_id: 'cn_added_1', model: 'draft-backup' }] }]);
    return route.fulfill(reply(saved));
  });
  await page.goto('/workspaces/team/services/models/coding');
  await page.getByRole('button', { name: 'Edit targets' }).click();
  const parent = page.getByRole('dialog', { name: 'Edit route · coding' });
  await parent.getByLabel('Upstream model').nth(1).fill('draft-backup');
  await parent.getByRole('button', { name: 'New Model Connection' }).nth(1).click();
  const child = page.getByRole('dialog', { name: 'New Connection' });
  await child.getByLabel('Name', { exact: true }).fill('Backup');
  await child.getByLabel('Credential reference', { exact: true }).fill('env:TOKEN');
  await child.getByRole('button', { name: 'Create Connection' }).click();
  await expect(child).toHaveCount(0);
  await expect(parent.getByRole('combobox', { name: 'Connection', exact: true }).nth(0)).toHaveValue('cn_existing');
  await expect(parent.getByRole('combobox', { name: 'Connection', exact: true }).nth(1)).toHaveValue('cn_added_1');
  expect(writes).toBe(0);
  await parent.getByRole('button', { name: 'Save route' }).click();
  await expect(parent).toHaveCount(0);
  expect(writes).toBe(1);
});

test('editing an MCP mount with new capacity clears the previous selected tools', async ({ page }) => {
  const state = await setup(page);
  state.connections.push({ id: 'cn_existing', display_name: 'Existing', kind: 'mcp', revision: '1' });
  await page.route('**/api/v1/workspaces/team/mcp-mounts', route => route.fulfill(reply([
    { alias: 'docs', connection_id: 'cn_existing', selected_tools: ['lookup'] },
  ])));
  await page.route('**/api/v1/connections/*/mcp-catalog', route => route.fulfill(reply([{ name: 'lookup', description: 'Read docs' }])));
  let mutations = 0;
  await page.route('**/api/v1/workspaces/team/mcp-mounts/docs?*', route => { mutations++; return route.fulfill(reply({}, 500)); });
  await page.goto('/workspaces/team/services/mcp');
  await page.getByRole('button', { name: 'Edit tools' }).click();
  const parent = page.getByRole('dialog', { name: 'Edit MCP source · docs' });
  await expect(parent.getByRole('checkbox', { name: 'lookup' })).toBeChecked();
  await parent.getByRole('button', { name: 'New MCP Connection' }).click();
  const child = page.getByRole('dialog', { name: 'New Connection' });
  await child.getByLabel('Name', { exact: true }).fill('New docs');
  await child.getByLabel('Endpoint').fill('https://mcp.example.test/mcp');
  await child.getByRole('button', { name: 'Create Connection' }).click();
  await expect(child).toHaveCount(0);
  await expect(parent.getByLabel('Namespace')).toHaveValue('docs');
  await expect(parent.getByLabel('MCP source')).toHaveValue('cn_added_1');
  await expect(parent.getByRole('checkbox', { name: 'lookup' })).not.toBeChecked();
  await expect(parent.getByRole('button', { name: 'Review source & tools' })).toBeDisabled();
  expect(mutations).toBe(0);
});

test('Search retains an unapplied fallback order when replacing its primary in place', async ({ page }) => {
  const state = await setup(page);
  for (const id of ['cn_primary', 'cn_first', 'cn_second']) state.connections.push({ id, display_name: id, kind: 'search', revision: '1' });
  let saves = 0;
  await page.route('**/api/v1/workspaces/team/search-route', route => {
    if (route.request().method() === 'GET') return route.fulfill(reply({ connection_id: 'cn_primary', fallback_connection_ids: ['cn_first', 'cn_second'] }));
    saves++;
    expect(route.request().postDataJSON()).toEqual({ connection_id: 'cn_added_1', fallback_connection_ids: ['cn_second', 'cn_first'] });
    return route.fulfill(reply(workspace()));
  });
  await page.goto('/workspaces/team/services/search');
  await page.getByRole('button', { name: 'Move cn_second earlier' }).click();
  await page.getByRole('button', { name: 'New primary Connection' }).click();
  const dialog = page.getByRole('dialog', { name: 'New Connection' });
  await dialog.getByLabel('Name', { exact: true }).fill('Replacement');
  await dialog.getByLabel('Endpoint').fill('https://search.example.test/search');
  await dialog.getByRole('button', { name: 'Create Connection' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByLabel('Primary Search Connection')).toHaveValue('cn_added_1');
  await expect(page.locator('.search-fallback-row').first()).toContainText('cn_second');
  expect(saves).toBe(0);
  await page.getByRole('button', { name: 'Save Search route' }).click();
  await expect.poll(() => saves).toBe(1);
});
