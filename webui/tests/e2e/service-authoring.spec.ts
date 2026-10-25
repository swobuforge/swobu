import { expect, test, type Page } from '@playwright/test';

function response(body: unknown, status = 200) {
  return { status, contentType: status >= 400 ? 'application/problem+json' : 'application/json',
    headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) };
}
function workspace(routes: unknown[] = [], services: string[] = ['models', 'mcp', 'search'], etag = 'g1-1') {
  return { slug: 'engineering', display_name: 'Engineering', management_etag: etag,
    management_revision: 1, routes, mcp_mounts: [],
    enabled_services: services, readiness: { llm: routes.length ? 'ready' : 'not_configured', mcp: 'not_configured' } };
}
const connections = [
  { id: 'cn_model', display_name: 'OpenAI account', kind: 'model', revision: '1' },
  { id: 'cn_mcp', display_name: 'GitHub MCP', kind: 'mcp', revision: '2' },
  { id: 'cn_search', display_name: 'Brave Search', kind: 'search', revision: '3' },
  { id: 'cn_search_2', display_name: 'SearXNG', kind: 'search', revision: '4' },
];
async function node(page: Page) {
  await page.route('**/api/v1/node', route => route.fulfill(response({ revision: 'a'.repeat(64), configuration_mode: 'managed',
    workspace_count: 1, connection_count: 4, principal_count: 0, publication_count: 0, budget_policy_count: 0 })));
  await page.route('**/api/v1/connections', route => route.fulfill(response(connections)));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(response([
    { kind: 'models', enabled: true, configured: false }, { kind: 'mcp', enabled: true, configured: false },
    { kind: 'search', enabled: true, configured: false },
  ])));
}

for (const pattern of ['coding', 'default']) {
test(`first Models route ${pattern} is a checked complete topology without replay identity`, async ({ page }) => {
  await node(page);
  let current = workspace();
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(response(current)));
  let puts = 0;
  await page.route(`**/api/v1/workspaces/engineering/model-routes/${pattern}`, async route => {
    if (route.request().method() !== 'PUT') return route.fulfill(response({}, 405));
    puts++;
    expect(route.request().headers()['if-match']).toBe('"g1-1"');
    expect(route.request().headers()['idempotency-key']).toBeUndefined();
    expect(route.request().postDataJSON()).toEqual({ tiers: [{ targets: [{
      id: 'primary', connection_id: 'cn_model', model: 'gpt-5', protocol: 'responses',
    }] }] });
    current = workspace([{ name: pattern, tiers: [{ targets: [{
      id: 'primary', connection_id: 'cn_model', model: 'gpt-5', protocol: 'responses',
    }] }] }], ['models', 'mcp', 'search'], 'g1-2');
    await route.fulfill(response(current));
  });
  await page.goto('/workspaces/engineering/services/models');
  await page.getByRole('button', { name: 'New route' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Model pattern').fill(pattern);
  await dialog.getByLabel('Connection').selectOption('cn_model');
  await dialog.getByLabel('Upstream model').fill('gpt-5');
  await dialog.getByRole('button', { name: 'Create route' }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('link', { name: pattern })).toBeVisible();
  expect(puts).toBe(1);
});
}

for (const outcome of ['committed', 'lost response']) {
test(`MCP mount requires exact review and reads current state after ${outcome}`, async ({ page }, testInfo) => {
  await node(page);
  let current = workspace();
  let reads = 0;
  await page.route('**/api/v1/workspaces/engineering', route => { reads++; return route.fulfill(response(current)); });
  const mount = { alias: 'github', connection_id: 'cn_mcp', selected_tools: ['search_code'] };
  let saved = false;
  await page.route('**/api/v1/workspaces/engineering/mcp-mounts', route => route.fulfill(response(saved ? [mount] : [])));
  await page.route('**/api/v1/connections/cn_mcp/mcp-catalog', route => route.fulfill(response([
    { name: 'search_code', description: 'Find code' }, { name: 'create_issue', description: 'Write issue' },
  ])));
  let previews = 0, applies = 0;
  await page.route('**/api/v1/workspaces/engineering/mcp-mounts/github?*', route => {
    const url = new URL(route.request().url());
    const body = route.request().postDataJSON();
    expect(route.request().method()).toBe('PUT');
    expect(body.connection_id).toBe('cn_mcp');
    expect(body.selected_tools).toEqual(['search_code']);
    if (url.searchParams.get('dry_run') === 'true') {
      previews++;
      expect(body.review).toBeUndefined();
      return route.fulfill(response({ workspace: 'engineering', alias: 'github', connection_id: 'cn_mcp',
        endpoint: 'https://example.com/mcp', execution_identity: 'Operator account', identity_verified: false,
        permission_summary: 'Repository read/write', allowed_tools: ['search_code'], potentially_effectful_tools: ['search_code'],
        affected_principals: [], exposures: [], revision: 'g1-1', access_revision: 'a', exposure_revision: 'e',
        digest: 'sha256:review', connection_revision: 'revision:2', approval_class: 'trusted_confirmation', warning: 'Review tools.',
      }));
    }
    applies++;
    expect(route.request().headers()['idempotency-key']).toBeUndefined();
    expect(body.review.digest).toBe('sha256:review');
    expect(body.review.workspace_etag).toBe('g1-1');
    expect(route.request().headers()['if-match']).toBe('"g1-1"');
    saved = true;
    current = { ...workspace([], ['models', 'mcp', 'search'], 'g1-2'), mcp_mounts: [mount] } as typeof current;
    return outcome === 'lost response' ? route.abort('connectionreset') : route.fulfill(response(current));
  });
  await page.goto('/workspaces/engineering/services/mcp');
  await page.getByRole('button', { name: 'Add server' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Namespace').fill('github');
  await dialog.getByLabel('MCP source').selectOption('cn_mcp');
  await dialog.getByRole('checkbox', { name: 'search_code' }).check();
  await dialog.getByRole('button', { name: 'Review source & tools' }).click();
  await expect(dialog.getByText('Repository read/write')).toBeVisible();
  expect(previews).toBe(1);
  expect(applies).toBe(0);
  await dialog.getByRole('button', { name: 'Apply reviewed mount' }).click();
  if (outcome === 'lost response') {
    await expect(dialog.getByRole('alert')).toContainText('Read the current Workspace');
    await expect(dialog.getByRole('button', { name: 'Apply reviewed mount' })).toBeDisabled();
    await dialog.screenshot({ path: testInfo.outputPath('uncertain-mount.png') });
    const beforeRead = reads;
    await dialog.getByRole('button', { name: 'Read current Workspace' }).click();
    await expect(dialog).toHaveCount(0);
    expect(reads).toBeGreaterThan(beforeRead);
  } else {
    await expect(dialog).toHaveCount(0);
  }
  await expect(page.getByText('github', { exact: true })).toBeVisible();
  expect(applies).toBe(1);
});
}

test('Direct Search routes primary and ordered fallbacks without changing Models', async ({ page }) => {
  await node(page);
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(response(workspace())));
  await page.route('**/api/v1/workspaces/engineering/search-route', route => {
    if (route.request().method() === 'GET') return route.fulfill(response({ type: 'about:blank',
      title: 'Not found', code: 'not_found', status: 404 }, 404));
    expect(route.request().method()).toBe('PUT');
    expect(route.request().headers()['if-match']).toBe('"g1-1"');
    expect(route.request().postDataJSON()).toEqual({ connection_id: 'cn_search', fallback_connection_ids: ['cn_search_2'] });
    return route.fulfill(response(workspace()));
  });
  await page.goto('/workspaces/engineering/services/search');
  await page.getByLabel('Primary Search Connection').selectOption('cn_search');
  await page.getByLabel('Add fallback Search Connection').selectOption('cn_search_2');
  await page.getByRole('button', { name: 'Save Search route' }).click();
  await expect(page.getByRole('heading', { name: 'Direct Search route' })).toBeVisible();
});

for (const outcome of [403, 412, 'lost acknowledgement'] as const) {
  test(`Search save ${outcome} requires current-state recovery`, async ({ page }) => {
    await node(page);
    let reads = 0, writes = 0;
    await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(response(workspace([], ['search'], `g1-${reads + 1}`))));
    await page.route('**/api/v1/workspaces/engineering/search-route', route => {
      if (route.request().method() === 'GET') { reads++; return route.fulfill(response({ connection_id: 'cn_search', fallback_connection_ids: [] })); }
      writes++;
      if (outcome === 'lost acknowledgement') return route.abort('connectionreset');
      return route.fulfill(response({ title: 'Search save rejected', code: outcome === 403 ? 'FORBIDDEN' : 'CONFLICT', status: outcome }, outcome));
    });
    await page.goto('/workspaces/engineering/services/search');
    await page.getByLabel('Add fallback Search Connection').selectOption('cn_search_2');
    await page.getByRole('button', { name: 'Save Search route' }).click();
    await expect(page.getByRole('alert')).toContainText('Do not retry');
    await expect(page.getByLabel('Primary Search Connection')).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Save Search route' })).toBeDisabled();
    expect(writes).toBe(1);
    await page.getByRole('button', { name: 'Read current Workspace' }).click();
    await expect(page.getByLabel('Primary Search Connection')).toBeEnabled();
    await expect(page.getByRole('button', { name: 'Save Search route' })).toBeDisabled();
    expect(reads).toBe(2); expect(writes).toBe(1);
  });
}

test('Search fallback ordering is a local draft, with accessible keyboard controls', async ({ page }) => {
  await node(page);
  await page.route('**/api/v1/connections', route => route.fulfill(response([...connections,
    { id: 'cn_search_3', display_name: 'Third Search', kind: 'search', revision: '1' },
  ])));
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(response(workspace())));
  let writes = 0;
  await page.route('**/api/v1/workspaces/engineering/search-route', route => {
    if (route.request().method() === 'GET') return route.fulfill(response({ connection_id: 'cn_search', fallback_connection_ids: ['cn_search_2', 'cn_search_3'] }));
    writes++;
    expect(route.request().postDataJSON()).toEqual({ connection_id: 'cn_search', fallback_connection_ids: ['cn_search_3', 'cn_search_2'] });
    return route.fulfill(response(workspace([], ['search'], 'g1-2')));
  });
  await page.goto('/workspaces/engineering/services/search');
  const earlier = page.getByRole('button', { name: 'Move cn_search_3 earlier' });
  await earlier.focus(); await expect(earlier).toBeFocused();
  expect(await earlier.evaluate(el => getComputedStyle(el).outlineStyle)).toBe('solid');
  await page.keyboard.press('Enter');
  expect(writes).toBe(0);
  await expect(page.locator('.search-fallback-row').first()).toContainText('Third Search');
  await page.getByRole('button', { name: 'Discard changes' }).click();
  await expect(page.locator('.search-fallback-row').first()).toContainText('cn_search_2');
  await page.getByRole('button', { name: 'Move cn_search_3 earlier' }).click();
  await page.getByRole('button', { name: 'Save Search route' }).click();
  await expect(page.getByRole('button', { name: 'Save Search route' })).toBeDisabled();
  expect(writes).toBe(1);
});

test('Search makes an unavailable selected Connection explicit and permits repair', async ({ page }) => {
  await node(page);
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(response(workspace())));
  await page.route('**/api/v1/workspaces/engineering/search-route', route => route.fulfill(response({ connection_id: 'cn_unavailable', fallback_connection_ids: ['cn_search_2'] })));
  await page.goto('/workspaces/engineering/services/search');
  await expect(page.getByLabel('Primary Search Connection')).toHaveValue('cn_unavailable');
  await expect(page.getByRole('option', { name: 'Unavailable Connection: cn_unavailable' })).toHaveCount(1);
  await page.getByLabel('Primary Search Connection').selectOption('cn_search');
  await expect(page.getByRole('button', { name: 'Save Search route' })).toBeEnabled();
});

for (const width of [390, 1440]) {
  test(`seven Search fallbacks remain inspectable at ${width}px`, async ({ page }, info) => {
    await node(page);
    await page.setViewportSize({ width, height: 930 });
    const candidates = Array.from({ length: 8 }, (_, i) => ({ id: `cn_dense_${i}`, display_name: `Long Search Connection ${i} with an explicit regional account identity`, kind: 'search', revision: '1' }));
    await page.route('**/api/v1/connections', route => route.fulfill(response(candidates)));
    await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(response(workspace())));
    await page.route('**/api/v1/workspaces/engineering/search-route', route => route.fulfill(response({ connection_id: candidates[0].id, fallback_connection_ids: candidates.slice(1).map(c => c.id) })));
    await page.goto('/workspaces/engineering/services/search');
    await expect(page.locator('.search-fallback-row')).toHaveCount(7);
    await expect(page.getByLabel('Add fallback Search Connection')).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.getByLabel('Primary Search Connection').selectOption(candidates[1].id);
    await expect(page.locator('.search-fallback-row')).toHaveCount(6);
    await expect(page.getByLabel('Add fallback Search Connection')).toBeVisible();
    await page.getByRole('button', { name: 'Discard changes' }).click();
    await expect(page.locator('.search-fallback-row')).toHaveCount(7);
    await page.screenshot({ path: info.outputPath('search-dense.png'), fullPage: true });
  });
}
