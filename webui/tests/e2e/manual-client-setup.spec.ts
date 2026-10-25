import { expect, test, type Page } from '@playwright/test';

function reply(body: unknown, status = 200) {
  return { status, contentType: status >= 400 ? 'application/problem+json' : 'application/json',
    headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) };
}
async function fixture(page: Page, services = ['models', 'mcp', 'search']) {
  let published = false;
  await page.route('**/api/v1/node', route => route.fulfill(reply({
    revision: 'a'.repeat(64), configuration_mode: 'managed', workspace_count: 1,
    connection_count: 1, principal_count: 1, publication_count: 0, budget_policy_count: 0,
  })));
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply({
    slug: 'engineering', display_name: 'Engineering', management_etag: 'ws-1',
    routes: services.includes('models') ? [{ name: 'coding', tiers: [{ targets: [{ id: 'one', connection_id: 'cn1', model: 'gpt', protocol: 'responses' }] }] }] : [],
    mcp_mounts: [], enabled_services: services,
  })));
  await page.route('**/api/v1/workspaces', route => route.fulfill(reply([{ slug: 'engineering', display_name: 'Engineering', route_count: 1 }])));
  await page.route('**/api/v1/principals', route => route.fulfill(reply([])));
  await page.route('**/api/v1/clients?*', route => route.fulfill(reply([])));
  await page.route('**/api/v1/workspaces/engineering/model-match?*', route => {
    const model = new URL(route.request().url()).searchParams.get('model');
    return route.fulfill(reply({ matched: model === 'coding', pattern: model === 'coding' ? 'coding' : '', priority: 1, tiers: 1, targets: 1 }));
  });
  const state = () => ({ workspace: 'engineering', address: 'relay',
    published, url: 'https://relay.example.test', revision: 'b'.repeat(64) });
  await page.route('**/api/v1/workspaces/engineering/publications', route => route.fulfill(reply(state())));
  await page.route('**/api/v1/workspaces/engineering/publications/relay', route => {
    published = route.request().method() === 'PUT';
    return route.fulfill(reply(state()));
  });
}

test('manual setup uses real Workspace route and exactly the advertised service address', async ({ page }) => {
  await fixture(page);
  await page.goto('/workspaces/engineering/connect');
  const setup = page.getByRole('region', { name: 'Manual client setup' });
  const example = setup.getByRole('group', { name: 'Client configuration example' });
  await expect(example).toContainText('model = "coding"');
  await expect(example).toContainText('/workspaces/engineering');
  await expect(example).toContainText('env_key = "SWOBU_API_KEY"');
  await setup.getByRole('combobox', { name: 'Client configuration' }).selectOption('claude');
  await expect(example).toContainText('CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY');
  await setup.getByRole('combobox', { name: 'Client location' }).selectOption('remote');
  await expect(example).toHaveCount(0);
  await expect(setup).toContainText('No valid published HTTPS address');
  await page.getByRole('button', { name: 'Publish via relay' }).click();
  await expect(example).toContainText('https://relay.example.test/workspaces/engineering');
  await expect(example).toContainText('<WORKSPACE_CREDENTIAL>');
  await page.getByRole('button', { name: 'Unpublish' }).click();
  await expect(example).toHaveCount(0);
  await expect(setup).not.toContainText('https://relay.example.test/workspaces/engineering');
});

test('no fabricated model and no secret-bearing setup after failed matcher, with readable mobile layout', async ({ page }) => {
  await fixture(page);
  await page.goto('/workspaces/engineering/connect');
  const setup = page.getByRole('region', { name: 'Manual client setup' });
  await setup.getByRole('combobox', { name: 'Manual model ID' }).fill('unconfigured');
  await expect(setup).toContainText('No saved Models route matches');
  await expect(setup.getByRole('button', { name: 'Copy example' })).toHaveCount(0);
  await setup.getByRole('combobox', { name: 'Manual model ID' }).fill('coding');
  await setup.getByRole('combobox', { name: 'Client configuration' }).selectOption('search');
  await expect(setup.getByRole('group', { name: 'Client configuration example' })).toContainText('/search?query=hello&limit=5');
  for (const width of [390, 759, 760, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
});

test('MCP-only Workspace has no Models config and shows only MCP connection fields', async ({ page }) => {
  await fixture(page, ['mcp']);
  await page.goto('/workspaces/engineering/connect');
  const setup = page.getByRole('region', { name: 'Manual client setup' });
  await expect(setup.getByRole('combobox', { name: 'Manual model ID' })).toHaveCount(0);
  await expect(setup.getByRole('combobox', { name: 'Client configuration' }).locator('option')).toHaveCount(1);
  await expect(setup).toContainText('streamable_http');
  await expect(setup).toContainText('Bearer <WORKSPACE_CREDENTIAL>');
  await expect(setup.getByRole('link', { name: 'Inspect real Activity' })).toHaveAttribute('href', /kind=mcp_operation/);
});

test('clipboard success and denial preserve manual recovery and keyboard access', async ({ page }) => {
  await fixture(page);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: {
      writeText: async (text: string) => { (window as unknown as { copiedExample: string }).copiedExample = text; },
    } });
  });
  await page.goto('/workspaces/engineering/connect');
  const setup = page.getByRole('region', { name: 'Manual client setup' });
  const copy = setup.getByRole('button', { name: 'Copy example' });
  await copy.focus();
  await expect(copy).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(setup).toContainText('Example copied.');
  expect(await page.evaluate(() => (window as unknown as { copiedExample: string }).copiedExample)).toContain('env_key = "SWOBU_API_KEY"');
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: async () => { throw new Error('Denied'); } } });
  });
  await copy.click();
  await expect(setup).toContainText('Clipboard unavailable. Select the example manually.');
  await expect(setup.locator('pre')).toContainText('model = "coding"');
  for (const width of [390, 759, 760, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: `test-results/manual-client-${width}.png`, fullPage: true });
  }
});

test('invalid publication and failed matcher never expose an executable model example', async ({ page }) => {
  await fixture(page);
  await page.route('**/api/v1/workspaces/engineering/publications', route => route.fulfill(reply({
    workspace: 'engineering', address: 'relay', published: true,
    url: 'http://invalid.example.test', revision: 'b'.repeat(64),
  })));
  let failed = true;
  await page.route('**/api/v1/workspaces/engineering/model-match?*', route => route.fulfill(failed
    ? reply({ type: 'about:blank', title: 'Unavailable', status: 503 }, 503)
    : reply({ matched: true, pattern: 'coding', priority: 1, tiers: 1, targets: 1 })));
  await page.goto('/workspaces/engineering/connect');
  const setup = page.getByRole('region', { name: 'Manual client setup' });
  await expect(setup).toContainText('Cannot verify this model ID');
  await expect(setup.getByRole('button', { name: 'Copy example' })).toHaveCount(0);
  failed = false;
  await setup.getByRole('button', { name: 'Check again' }).click();
  await expect(setup.getByRole('button', { name: 'Copy example' })).toBeVisible();
  await setup.getByRole('combobox', { name: 'Client location' }).selectOption('remote');
  await expect(setup).toContainText('No valid published HTTPS address');
  await expect(setup.getByRole('group', { name: 'Client configuration example' })).toHaveCount(0);
});

test('a failed fresh route check removes a previously successful example', async ({ page }) => {
  await fixture(page);
  let failed = false;
  await page.route('**/api/v1/workspaces/engineering/model-match?*', route => route.fulfill(failed
    ? reply({ type: 'about:blank', title: 'Unavailable', status: 503 }, 503)
    : reply({ matched: true, pattern: 'coding', priority: 1, tiers: 1, targets: 1 })));
  await page.goto('/workspaces/engineering/connect');
  const setup = page.getByRole('region', { name: 'Manual client setup' });
  await expect(setup.getByRole('button', { name: 'Copy example' })).toBeVisible();
  failed = true;
  const model = setup.getByPlaceholder('Choose a model ID that matches a saved route');
  await model.fill('another');
  await expect(setup).toContainText('Cannot verify this model ID');
  await model.fill('coding');
  await expect(setup).toContainText('Cannot verify this model ID');
  await expect(setup.getByRole('button', { name: 'Copy example' })).toHaveCount(0);
});

test('a new observed Workspace revision discards the previous model match', async ({ page }) => {
  await fixture(page);
  let revision = 'ws-1';
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply({
    slug: 'engineering', display_name: 'Engineering', management_etag: revision,
    routes: [{ name: 'coding', tiers: [] }], mcp_mounts: [], enabled_services: ['models'],
  })));
  await page.route('**/api/v1/workspaces/engineering/model-match?*', route => route.fulfill(reply({
    matched: revision === 'ws-1', pattern: revision === 'ws-1' ? 'coding' : '', priority: 1, tiers: 1, targets: 1,
  })));
  await page.goto('/workspaces/engineering/connect');
  const setup = page.getByRole('region', { name: 'Manual client setup' });
  await expect(setup.getByRole('button', { name: 'Copy example' })).toBeVisible();
  revision = 'ws-2';
  await page.getByRole('link', { name: 'Overview', exact: true }).click();
  await page.getByRole('link', { name: 'Connect', exact: true }).click();
  await expect(setup).toContainText('No saved Models route matches');
  await expect(setup.getByRole('button', { name: 'Copy example' })).toHaveCount(0);
});

test('embedded Operator derives manual MCP configuration from real enabled Workspace state', async ({ page, playwright }) => {
  const origin = 'http://127.0.0.1:4179';
  const slug = 'manual-client-proof';
  const native = await playwright.request.newContext({ userAgent: 'SwobuBrowserProofNative' });
  try {
    const created = await native.post(`${origin}/api/v1/workspaces`, {
      headers: { 'Idempotency-Key': 'manual-client-proof-create' }, data: { slug },
    });
    expect(created.status()).toBe(201);
    const workspace = await native.get(`${origin}/api/v1/workspaces/${slug}`);
    const enabled = await native.put(`${origin}/api/v1/workspaces/${slug}/services/mcp`, {
      headers: { 'If-Match': workspace.headers().etag },
    });
    expect(enabled.status()).toBe(200);
    const launch = await native.post(`${origin}/api/v1/browser-launches`);
    expect(launch.status()).toBe(201);
    await page.goto(origin + (await launch.json()).launch_path);
    await page.goto(`${origin}/workspaces/${slug}/connect`);
    const setup = page.getByRole('region', { name: 'Manual client setup' });
    await expect(setup.locator('pre')).toBeVisible();
    const recipe = JSON.parse((await setup.locator('pre').textContent())!);
    expect(recipe).toEqual({ transport: 'streamable_http', url: `${origin}/workspaces/${slug}/mcp`,
      headers: { Authorization: 'Bearer <WORKSPACE_CREDENTIAL>' } });
    await expect(setup.getByRole('combobox', { name: 'Client configuration' }).locator('option')).toHaveCount(1);
    await setup.getByRole('combobox', { name: 'Client location' }).selectOption('remote');
    await expect(setup).toContainText('No valid published HTTPS address');
    await expect(setup.locator('pre')).toHaveCount(0);
  } finally {
    await native.dispose();
  }
});

test('failed publication refresh removes cached remote configuration', async ({ page }) => {
  await fixture(page, ['mcp']);
  let failed = false;
  await page.route('**/api/v1/workspaces/engineering/publications', route => route.fulfill(failed
    ? reply({ type: 'about:blank', title: 'Unavailable', status: 503 }, 503)
    : reply({ workspace: 'engineering', address: 'relay', published: true,
      url: 'https://relay.example.test', revision: 'b'.repeat(64) })));
  await page.goto('/workspaces/engineering/connect');
  const setup = page.getByRole('region', { name: 'Manual client setup' });
  await setup.getByRole('combobox', { name: 'Client location' }).selectOption('remote');
  await expect(setup.locator('pre')).toContainText('https://relay.example.test');
  failed = true;
  await page.getByRole('link', { name: 'Overview', exact: true }).click();
  await page.getByRole('link', { name: 'Connect', exact: true }).click();
  await expect(page.getByText('Publication status unavailable.', { exact: false })).toBeVisible();
  await setup.getByRole('combobox', { name: 'Client location' }).selectOption('remote');
  await expect(setup.locator('pre')).toHaveCount(0);
});
