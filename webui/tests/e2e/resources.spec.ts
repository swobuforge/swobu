import { expect, test, type Page } from '@playwright/test';
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { PNG } from 'pngjs';

// These are browser transport fixtures, not application resource state. The
// production source never imports them or manufactures a connected Node.
function reply(body: unknown, status = 200, headers: Record<string, string> = {}) {
  return { status, contentType: status >= 400 ? 'application/problem+json' : 'application/json',
    headers: { 'Cache-Control': 'no-store', ...headers }, body: JSON.stringify(body) };
}
const workspace = (etag = 'gen-1', enabled = ['models']) => ({
  slug: 'engineering', display_name: 'Engineering', management_etag: etag, management_revision: 1, routes: [], mcp_mounts: [], enabled_services: enabled,
  readiness: { llm: 'not_configured', mcp: 'disabled' },
});
async function node(page: Page, mode = 'managed') {
  await page.route('**/api/v1/node', route => route.fulfill(reply({ revision: 'a'.repeat(64), configuration_mode: mode,
    workspace_count: 1, connection_count: 1, principal_count: 0, publication_count: 0, budget_policy_count: 0 })));
}

for (const width of [390, 760, 1440]) {
  test(`live Workspace collection and service state at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 950 });
    await node(page);
    await page.route('**/api/v1/workspaces', route => route.fulfill(reply([{ slug: 'engineering', display_name: 'Engineering', route_count: 0 }])));
    await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply(workspace())));
    await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([
      { kind: 'models', enabled: true, configured: false }, { kind: 'mcp', enabled: false, configured: false },
      { kind: 'search', enabled: false, configured: false },
    ])));
    await page.goto('/workspaces');
    await expect(page.getByRole('link', { name: /Engineering/ })).toBeVisible();
    await page.getByRole('link', { name: /Engineering/ }).click();
    await expect(page.getByRole('heading', { name: 'Engineering', level: 1 })).toBeVisible();
    await page.getByRole('navigation', { name: 'Workspace sections' }).getByRole('link', { name: 'Services' }).click();
    await expect(page.getByText('Enabled · not configured')).toBeVisible();
    await expect(page.getByText('Disabled')).toHaveCount(2);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
}

test('configured Services offer their real authoring page, never an impossible Disable request', async ({ page }) => {
  await node(page);
  let mutationCalls = 0;
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply(workspace())));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([
    { kind: 'models', enabled: true, configured: true },
    { kind: 'mcp', enabled: true, configured: false },
    { kind: 'search', enabled: false, configured: false },
  ])));
  await page.route('**/api/v1/workspaces/engineering/services/models', route => {
    mutationCalls++;
    return route.fulfill(reply({ error: 'should not mutate a configured Service' }, 500));
  });
  await page.goto('/workspaces/engineering/services');
  const models = page.locator('.servicecontrol').filter({ has: page.getByText('Models', { exact: true }) });
  await expect(models.getByRole('button', { name: 'Disable' })).toHaveCount(0);
  await expect(models.getByRole('link', { name: 'Open' })).toHaveAttribute('href', '/workspaces/engineering/services/models');
  const mcp = page.locator('.servicecontrol').filter({ has: page.getByText('MCP', { exact: true }) });
  await expect(mcp.getByRole('button', { name: 'Disable' })).toBeVisible();
  await expect(mcp.getByRole('link', { name: 'Configure' })).toHaveAttribute('href', '/workspaces/engineering/services/mcp');
  expect(mutationCalls).toBe(0);
});

test('Workspace create sends a single explicit retry identity; edit changes the intent', async ({ page }) => {
  await node(page);
  let created = false;
  const keys: string[] = [];
  await page.route('**/api/v1/workspaces', async route => {
    if (route.request().method() === 'GET') return route.fulfill(reply(created ? [{ slug: 'engineering', display_name: 'Engineering', route_count: 0 }] : []));
    keys.push(route.request().headers()['idempotency-key']);
    if (keys.length === 1) return route.fulfill(reply({ type: 'about:blank', title: 'Unavailable', status: 503, code: 'UNAVAILABLE' }, 503));
    expect(route.request().postDataJSON()).toEqual({ slug: 'engineering', display_name: 'Engineering' });
    created = true;
    return route.fulfill(reply(workspace(), 201, { ETag: '"gen-1"' }));
  });
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply(workspace())));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([{ kind: 'models', enabled: true, configured: false }, { kind: 'mcp', enabled: false, configured: false }, { kind: 'search', enabled: false, configured: false }])));
  await page.goto('/workspaces');
  await page.getByRole('button', { name: 'New Workspace' }).click();
  await page.getByRole('dialog').getByRole('textbox', { name: 'Workspace address (permanent)' }).fill('engineering');
  await page.getByRole('dialog').getByRole('textbox', { name: 'Display name' }).fill('Engineering');
  await page.getByRole('button', { name: 'Create Workspace' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await page.getByRole('button', { name: 'Retry same creation' }).click();
  await expect(page).toHaveURL(/\/workspaces\/engineering\/overview$/);
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
});

test('service mutation carries the reviewed ETag and never retries a conflict', async ({ page }) => {
  await node(page);
  let revision = 'gen-1';
  let requestCount = 0;
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply(workspace(revision))));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([
    { kind: 'models', enabled: true, configured: false }, { kind: 'mcp', enabled: false, configured: false }, { kind: 'search', enabled: false, configured: false },
  ])));
  await page.route('**/api/v1/workspaces/engineering/services/mcp', route => {
    requestCount++;
    expect(route.request().headers()['if-match']).toBe('"gen-1"');
    return route.fulfill(reply({ type: 'about:blank', title: 'Changed', status: 412, code: 'REVISION_CONFLICT' }, 412));
  });
  await page.goto('/workspaces/engineering/services');
  await page.getByRole('button', { name: 'Enable' }).first().click();
  await expect(page.getByRole('alert')).toContainText('Reload');
  expect(requestCount).toBe(1);
  revision = 'gen-2';
});

test('Connections are live upstream resources, separate from Workspace Services', async ({ page }) => {
  await node(page);
  await page.route('**/api/v1/connections', route => route.fulfill(reply([{
    id: 'cn_openai', kind: 'model', display_name: 'Shared OpenAI', revision: '1',
    created_at: '2026-10-17T12:00:00Z', updated_at: '2026-10-17T12:00:00Z',
    spec: { shape: 'standard', provider_id: 'openai', authentication_configured: true },
  }])));
  await page.route('**/api/v1/connections/cn_openai', route => route.fulfill(reply({
    id: 'cn_openai', kind: 'model', display_name: 'Shared OpenAI', revision: '1',
    created_at: '2026-10-17T12:00:00Z', updated_at: '2026-10-17T12:00:00Z',
    spec: { shape: 'standard', provider_id: 'openai', authentication_configured: true },
  }, 200, { ETag: '"1"' })));
  await page.route('**/api/v1/connections/cn_openai/references', route => route.fulfill(reply({
    connection_id: 'cn_openai', connection_revision: '1', consumers: [{ workspace: 'engineering', kind: 'model', route: 'coding', target: 't1' }],
  })));
  await page.goto('/connections');
  await expect(page.getByRole('link', { name: /Shared OpenAI/ })).toBeVisible();
  await page.getByRole('link', { name: /Shared OpenAI/ }).click();
  await expect(page.getByRole('heading', { name: 'Shared OpenAI' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'engineering' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delete Connection' })).toBeDisabled();
});

test('externally managed Node disables authoring controls without hiding resource reads', async ({ page }) => {
  await node(page, 'externally_managed');
  await page.route('**/api/v1/workspaces', route => route.fulfill(reply([])));
  await page.goto('/workspaces');
  await expect(page.getByRole('button', { name: 'New Workspace' })).toBeDisabled();
  await expect(page.getByRole('heading', { name: 'No Workspaces yet' })).toBeVisible();
});


test('Connection probe is explicit and tied to its reviewed revision', async ({ page }) => {
  await node(page);
  const upstream = { id: 'cn_openai', kind: 'model', display_name: 'Shared OpenAI', revision: '7',
    created_at: '2026-10-17T12:00:00Z', updated_at: '2026-10-17T12:00:00Z',
    spec: { shape: 'standard', provider_id: 'openai', authentication_configured: true } };
  await page.route('**/api/v1/connections/cn_openai', route => route.fulfill(reply(upstream, 200, { ETag: '"7"' })));
  await page.route('**/api/v1/connections/cn_openai/references', route => route.fulfill(reply({
    connection_id: 'cn_openai', connection_revision: '7', consumers: [],
  })));
  let calls = 0;
  await page.route('**/api/v1/connections/cn_openai/checks', route => {
    calls++;
    expect(route.request().method()).toBe('POST');
    expect(route.request().headers()['if-match']).toBe('"7"');
    return route.fulfill(reply({
      connection_id: 'cn_openai', connection_revision: '7', checked_at: '2026-10-18T12:00:00Z',
      reachability: 'reachable', authentication: 'accepted', capability_summary: ['models'],
    }));
  });
  await page.goto('/connections/cn_openai');
  expect(calls).toBe(0); // Upstream probing is never implicit on page load.
  await page.getByRole('button', { name: 'Check upstream' }).click();
  await expect(page.getByText('accepted')).toBeVisible();
  expect(calls).toBe(1);
});

for (const width of [390, 759, 760, 761, 1440]) {
test(`Workspace Overview uses real service, access, publication and Activity summaries at ${width}px`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width, height: 950 });
  await page.clock.install({ time: new Date('2026-10-20T19:00:00Z') });
  await node(page);
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply({
    ...workspace('gen-3', ['models', 'mcp', 'search']),
    routes: [{ name: 'coding', tiers: [{ targets: [{ id: 'az-1', connection_id: 'cn_azure', model: 'gpt-5', protocol: 'openai' }] }] }],
    mcp_mounts: [{ alias: 'node', builtin: 'swobu-admin', connection_id: '', allowed_tools: ['node_status'] }],
  })));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([
    { kind: 'models', enabled: true, configured: true },
    { kind: 'mcp', enabled: true, configured: true },
    { kind: 'search', enabled: true, configured: false },
  ])));
  await page.route('**/api/v1/principals', route => route.fulfill(reply([
    { id: 'p_1', label: 'Build', kind: 'service', status: 'active', revision: 'r1', permissions: [
      { workspace: 'engineering', action: 'use', services: ['models', 'mcp'] }] },
    { id: 'p_expired', label: 'Expired', kind: 'service', status: 'active', revision: 'r1', permissions: [
      { workspace: 'engineering', action: 'use', expires_at: '2026-10-20T18:59:59Z' }] },
    { id: 'p_other', label: 'Other Workspace', kind: 'service', status: 'active', revision: 'r1', permissions: [
      { workspace: 'other', action: 'use' }] },
    { id: 'p_2', label: 'Revoked', kind: 'service', status: 'disabled', revision: 'r1', permissions: [
      { workspace: 'engineering', action: 'use', services: ['models'] }] },
  ])));
  await page.route('**/api/v1/workspaces/engineering/publications', route => route.fulfill(reply({
    workspace: 'engineering', address: 'relay', published: true, url: 'https://relay.example', revision: 'r-1',
  })));
  await page.route('**/api/v1/activity?**', route => route.fulfill(reply({
    scope: { kind: 'workspace', workspace: 'engineering' },
    exchanges: [{ exchange_id: 'ex_123', access_id: 'acc', workspace: 'engineering',
      started_at: '2026-10-20T18:00:00Z', state: 'succeeded', attempt_count: 1,
      route: 'coding', provider: 'OpenAI' }], mcp_operations: [],
  })));
  await page.goto('/workspaces/engineering/overview');
  await expect(page.getByRole('heading', { name: 'Engineering', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Workspace policy' })).toBeVisible();
  await expect(page.getByText('1 route · 1 target')).toBeVisible();
  await expect(page.getByText('1 MCP source')).toBeVisible();
  await expect(page.getByText('No Search source')).toBeVisible();
  await expect(page.getByText('Models · coding → OpenAI')).toBeVisible();
  await expect(page.getByText('Published via Swobu relay')).toBeVisible();
  await expect(page.getByText('Availability not verified', { exact: false })).toBeVisible();
  await expect(page.locator('.workspace-policy-metric').first().locator('strong')).toHaveText('1');
  await expect(page.locator('.workspace-policy-metric').last().locator('strong')).toHaveText('3');
  await expect(page.getByRole('heading', { name: 'Model routes' })).toHaveCount(0);
  await expect(page.locator('.servicerow').first()).toHaveAttribute('href', '/workspaces/engineering/services/models');
  await expect(page.getByRole('link', { name: /Inspect Activity/ })).toHaveAttribute('href', /workspace=engineering/);
  await expect(page.getByRole('navigation', { name: 'Workspace sections' }).getByRole('link')).toHaveText(['Overview', 'Services', 'Access', 'Limits', 'Connect']);
  await expect(page.getByRole('navigation', { name: 'Primary' }).getByRole('link')).toHaveText(['Workspaces', 'Connections', 'Access', 'Activity', 'Settings']);
  await expect(page.locator('.workspace-policy-metric').first()).toHaveCSS('border-left-width', '4px');
  await expect(page.locator('.workspace-reachability')).toHaveCSS('border-left-width', '4px');
  await page.screenshot({ path: testInfo.outputPath('overview.png'), fullPage: true });
  // Equal product topology in the frozen v5 prototype; current runtime copy
  // (built-in MCP, direct Search, relay publication) remains production-owned.
  const reference = await page.context().newPage();
  await reference.setViewportSize({ width, height: 950 });
  await reference.goto('/');
  const referencePath = resolve(import.meta.dirname, '../../../../..', 'docs/00-inbox/Swobu_Pivot_Closeout_War_Room_2026-10-15/reference/Swobu_Operator_Ergonomics_v5.html');
  await reference.setContent(await readFile(referencePath, 'utf8'));
  await reference.evaluate(`{
    const ws = store.workspaces.find(w => w.id === 'engineering');
    ws.models = [{ name: 'coding', tiers: [{ targets: [{ id: 'az-1', provider: 'OpenAI', model: 'gpt-5', connection: 'azure' }] }] }];
    ws.mcp = [{ alias: 'node', server: 'swobu-admin', allow: [{ name: 'node_status' }] }];
    ws.search = null; ws.operations = { models: true, mcp: true, search: true };
    ws.grants = [{ person: 'build', ops: ['models', 'mcp'] }];
    ws.remoteOrigins = ['tailnet']; ws.recent = '10/20/2026, 6:00:00 PM';
    store.history = [{ ws: 'engineering', kind: 'Models', route: 'coding', provider: 'OpenAI' }];
    location.hash = '#workspaces/engineering/overview';
  }`);
  await expect(reference.getByRole('heading', { name: 'Workspace policy' })).toBeVisible();
  for (const selector of ['.rail', '.brand', '.nav', '.top', '.content', '.findbtn']) {
    const actual = await page.locator(selector).boundingBox();
    const accepted = await reference.locator(selector).boundingBox();
    expect(actual).not.toBeNull(); expect(accepted).not.toBeNull();
    for (const key of ['x', 'y', 'width'] as const) expect(Math.abs(actual![key] - accepted![key]), `${selector}.${key}`).toBeLessThan(1);
  }
  const actual = PNG.sync.read(await page.screenshot());
  const accepted = PNG.sync.read(await reference.screenshot({ path: testInfo.outputPath('overview-reference.png') }));
  const diff = new PNG({ width: actual.width, height: actual.height });
  for (let i = 0; i < actual.data.length; i += 4) {
    const changed = actual.data.subarray(i, i + 3).some((value, channel) => value !== accepted.data[i + channel]);
    diff.data.set(changed ? [220, 30, 70, 255] : [245, 245, 245, 255], i);
  }
  await writeFile(testInfo.outputPath('overview-reference-diff.png'), PNG.sync.write(diff));
  await reference.close();
  const rename = page.getByRole('button', { name: 'Rename', exact: true });
  await rename.focus();
  await expect(rename).toBeFocused();
  expect(await rename.evaluate(el => getComputedStyle(el).outlineStyle)).toBe('solid');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(rename).toBeFocused();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

}

test('Workspace Overview never invents activity, grants or publication when evidence is unavailable', async ({ page }) => {
  await node(page);
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply(workspace())));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([
    { kind: 'models', enabled: true, configured: false }, { kind: 'mcp', enabled: false, configured: false },
    { kind: 'search', enabled: false, configured: false },
  ])));
  await page.route('**/api/v1/principals', route => route.fulfill(reply({ title: 'Unavailable', status: 503 }, 503)));
  await page.route('**/api/v1/workspaces/engineering/publications', route => route.fulfill(reply({ title: 'Unavailable', status: 503 }, 503)));
  await page.route('**/api/v1/activity?**', route => route.fulfill(reply({ scope: { kind: 'workspace', workspace: 'engineering' }, exchanges: [], mcp_operations: [] })));
  await page.goto('/workspaces/engineering/overview');
  await expect(page.getByText('No operations in the current snapshot')).toBeVisible();
  await expect(page.getByText('Cannot load Workspace access summary.')).toBeVisible();
  await expect(page.getByText('Cannot load publication status.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Rename' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Rename' })).toBeVisible();
});

for (const state of ['loading', 'unavailable', 'empty', 'mcp', 'dense'] as const) {
  test(`Workspace Overview represents ${state} projections without inventing state`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 950 });
    await node(page, 'external');
    await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply({ ...workspace(), display_name: state === 'dense' ? 'Engineering'.repeat(10) : 'Engineering' })));
    await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([
      { kind: 'models', enabled: true, configured: false },
      { kind: 'mcp', enabled: false, configured: false }, { kind: 'search', enabled: false, configured: false },
    ])));
    let release!: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    await page.route('**/api/v1/principals', async route => {
      if (state === 'loading') await held;
      return route.fulfill(reply([]));
    });
    await page.route('**/api/v1/workspaces/engineering/publications', async route => {
      if (state === 'loading') await held;
      return route.fulfill(reply({ workspace: 'engineering', address: 'relay', published: false, revision: 'r1' }));
    });
    await page.route('**/api/v1/activity?**', async route => {
      if (state === 'loading') await held;
      if (state === 'unavailable') return route.fulfill(reply({ title: 'Unavailable', status: 503 }, 503));
      return route.fulfill(reply({ scope: { kind: 'workspace', workspace: 'engineering' },
        exchanges: state === 'mcp' ? [{ exchange_id: 'ex_old', workspace: 'engineering', started_at: '2026-10-20T18:00:00Z', route: 'older', provider: 'OpenAI' }] : [],
        mcp_operations: state === 'mcp' || state === 'dense' ? [{ operation_id: 'mcp_new', workspace: 'engineering', started_at: '2026-10-20T18:00:00.100Z', source: 'node', tool: state === 'dense' ? 'node_status'.repeat(20) : 'node_status' }] : [],
      }));
    });
    await page.goto('/workspaces/engineering/overview');
    await expect(page.getByRole('heading', { name: 'Workspace policy' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Rename', exact: true })).toHaveCount(0);
    await expect(page.locator('.servicerow')).toHaveCount(1);
    try {
      if (state === 'loading') {
        await expect(page.locator('.workspace-recent')).toContainText('Loading…');
        await expect(page.locator('.workspace-policy-metric').first().locator('strong')).toHaveText('…');
        await expect(page.locator('.workspace-reachability')).toContainText('Checking…');
      } else {
        await expect(page.locator('.workspace-policy-metric').first().locator('strong')).toHaveText('0');
        await expect(page.locator('.workspace-reachability')).toContainText('Local only');
        await expect(page.locator('.workspace-recent')).toContainText(state === 'unavailable' ? 'Unavailable' :
          state === 'mcp' || state === 'dense' ? 'MCP · node.node_status' : 'No operations in the current snapshot');
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      }
    } finally { release(); }
  });
}

test('Workspace header rename binds the current revision and preserves the address', async ({ page }) => {
  await node(page);
  let name = 'Engineering';
  let calls = 0;
  await page.route('**/api/v1/workspaces/engineering', route => {
    if (route.request().method() === 'PATCH') {
      calls++;
      expect(route.request().headers()['if-match']).toBe('"gen-1"');
      expect(route.request().postDataJSON()).toEqual({ display_name: 'Engineering renamed' });
      name = 'Engineering renamed';
    }
    return route.fulfill(reply({ ...workspace(), display_name: name }));
  });
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([])));
  await page.route('**/api/v1/principals', route => route.fulfill(reply([])));
  await page.route('**/api/v1/workspaces/engineering/publications', route => route.fulfill(reply({ workspace: 'engineering', published: false })));
  await page.route('**/api/v1/activity?**', route => route.fulfill(reply({ scope: { kind: 'workspace', workspace: 'engineering' }, exchanges: [] })));
  await page.goto('/workspaces/engineering/overview');
  await page.getByRole('button', { name: 'Rename', exact: true }).click();
  await page.getByRole('textbox', { name: 'Display name' }).fill('Engineering renamed');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Engineering renamed', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/workspaces\/engineering\/overview$/);
  expect(calls).toBe(1);
});

test('Connection credentials rotate in place with the original revision and exact retry identity', async ({ page }) => {
  await node(page);
  let revision = '7';
  const connection = () => ({ id: 'cn_openai', kind: 'model', display_name: 'Shared OpenAI', revision,
    created_at: '2026-10-20T12:00:00Z', updated_at: '2026-10-20T12:00:00Z',
    spec: { shape: 'standard', provider_id: 'openai', authentication_configured: true },
  });
  await page.route('**/api/v1/connections/cn_openai', route => route.fulfill(reply(connection(), 200, { ETag: `"${revision}"` })));
  await page.route('**/api/v1/connections/cn_openai/references', route => route.fulfill(reply({ connection_id: 'cn_openai', connection_revision: revision, consumers: [] })));
  let calls = 0;
  let initialKey = '';
  let releaseResponse!: () => void;
  const responseGate = new Promise<void>(resolve => { releaseResponse = resolve; });
  await page.route('**/api/v1/connections/cn_openai/credential-replacements', async route => {
    const request = route.request();
    calls++;
    expect(request.method()).toBe('POST');
    expect(request.headers()['if-match']).toBe('"7"');
    const key = request.headers()['idempotency-key'];
    if (calls === 1) initialKey = key;
    else expect(key).toBe(initialKey);
    expect(request.postDataJSON()).toEqual({ secret: 'new-secret-material', same_identity: true });
    if (calls === 1) {
      await responseGate;
      return route.abort('failed');
    }
    revision = '8';
    return route.fulfill(reply({ connection: connection(), replayed: true }));
  });
  await page.goto('/connections/cn_openai');
  await page.getByRole('button', { name: 'Replace credential' }).click();
  const dialog = page.getByRole('dialog', { name: 'Replace credential' });
  await dialog.getByLabel('New secret').fill('new-secret-material');
  await dialog.getByRole('checkbox', { name: /external identity is unchanged/ }).check();
  expect(calls).toBe(0);
  await dialog.getByRole('button', { name: 'Replace credential' }).click();
  try {
    await expect(dialog.getByLabel('New secret')).toBeDisabled();
    await expect(dialog.getByRole('checkbox')).toBeDisabled();
  } finally { releaseResponse(); }
  await expect(dialog.getByRole('button', { name: 'Retry exact replacement' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Retry exact replacement' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText('8', { exact: true })).toBeVisible();
  expect(calls).toBe(2);
  expect(await page.getByText('new-secret-material').count()).toBe(0);
});

test('Connection edit sends a complete private draft with same-identity assertion and strong revision', async ({ page }) => {
  await node(page);
  let revision = '7';
  const connection = () => ({ id: 'cn_openai', kind: 'model', display_name: revision === '7' ? 'Old name' : 'New name', revision,
    created_at: '2026-10-20T12:00:00Z', updated_at: '2026-10-20T12:00:00Z',
    spec: { shape: 'standard', provider_id: 'openai', endpoint: 'https://api.example.test/v1', authentication_configured: true },
  });
  await page.route('**/api/v1/connections/cn_openai', route => route.fulfill(reply(connection(), 200, { ETag: `"${revision}"` })));
  await page.route('**/api/v1/connections/cn_openai/references', route => route.fulfill(reply({ connection_id: 'cn_openai', connection_revision: revision, consumers: [] })));
  let updates = 0;
  await page.route('**/api/v1/connections/cn_openai', route => {
    const request = route.request();
    if (request.method() === 'GET') return route.fulfill(reply(connection(), 200, { ETag: `"${revision}"` }));
    updates++;
    expect(request.method()).toBe('PATCH');
    expect(request.headers()['if-match']).toBe('"7"');
    expect(request.postDataJSON()).toEqual({
      kind: 'model', display_name: 'New name', same_identity: true,
      spec: { shape: 'standard', provider_id: 'openai', configuration: {
        endpoint: 'https://api.example.test/v2', credential_ref: 'env:PROVIDER_API_KEY',
      } },
    });
    revision = '8';
    return route.fulfill(reply(connection(), 200, { ETag: '"8"' }));
  });
  await page.goto('/connections/cn_openai');
  await page.getByRole('button', { name: 'Edit configuration' }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit Old name' });
  await dialog.getByRole('textbox', { name: 'Name' }).fill('New name');
  await dialog.getByRole('textbox', { name: /Endpoint/ }).fill('https://api.example.test/v2');
  await dialog.getByRole('textbox', { name: /Credential reference/ }).fill('env:PROVIDER_API_KEY');
  await dialog.getByRole('checkbox', { name: /same external account or server/ }).check();
  await dialog.getByRole('button', { name: 'Save Connection' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'New name' })).toBeVisible();
  expect(updates).toBe(1);
});

test('Stale Connection edit locks its draft until explicit reload; externally managed Node cannot mutate', async ({ page }) => {
  await node(page);
  const c = { id: 'cn_search', kind: 'search', display_name: 'Search A', revision: '3',
    created_at: '2026-10-20T12:00:00Z', updated_at: '2026-10-20T12:00:00Z',
    spec: { provider_id: 'brave', endpoint: 'https://search.example.test', authentication_configured: false },
  };
  await page.route('**/api/v1/connections/cn_search', route => route.fulfill(reply(c, 200, { ETag: '"3"' })));
  await page.route('**/api/v1/connections/cn_search/references', route => route.fulfill(reply({ connection_id: 'cn_search', connection_revision: '3', consumers: [] })));
  let calls = 0;
  await page.route('**/api/v1/connections/cn_search', route => {
    if (route.request().method() === 'GET') return route.fulfill(reply(c));
    calls++;
    return route.fulfill(reply({ type: 'about:blank', title: 'Changed', status: 412, code: 'REVISION_CONFLICT' }, 412));
  });
  await page.goto('/connections/cn_search');
  await page.getByRole('button', { name: 'Edit configuration' }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit Search A' });
  await dialog.getByRole('textbox', { name: 'Endpoint' }).fill('https://search.example.test');
  await dialog.getByRole('checkbox', { name: /same external account or server/ }).check();
  await dialog.getByRole('button', { name: 'Save Connection' }).click();
  await expect(dialog.getByRole('button', { name: 'Save Connection' })).toBeDisabled();
  expect(calls).toBe(1);
  await dialog.getByRole('button', { name: 'Inspect current Connection' }).click();
  await expect(dialog).toHaveCount(0);
  await node(page, 'externally_managed');
  await page.reload();
  await expect(page.getByRole('button', { name: 'Edit configuration' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Replace credential' })).toBeDisabled();
});

test('MCP stdio replacement retains the selected environment through uncertain delivery', async ({ page }) => {
  await node(page);
  await page.setViewportSize({ width: 390, height: 640 });
  const connection = { id: 'cn_stdio', kind: 'mcp', display_name: 'Private stdio', revision: '4',
    created_at: '2026-10-20T12:00:00Z', updated_at: '2026-10-20T12:00:00Z',
    spec: { transport: 'stdio', executable_name: 'node', argument_count: 2, environment_binding_count: 2, execution_identity: 'local', permission_summary: 'read', identity_verified: false },
  };
  await page.route('**/api/v1/connections/cn_stdio', route => route.fulfill(reply(connection)));
  await page.route('**/api/v1/connections/cn_stdio/references', route => route.fulfill(reply({ connection_id: connection.id, connection_revision: '4', consumers: [] })));
  let calls = 0;
  let firstKey = '';
  await page.route('**/api/v1/connections/cn_stdio/credential-replacements', route => {
    calls++;
    expect(route.request().postDataJSON()).toEqual({ secret: 'synthetic-stdio-secret', environment: 'TOKEN', same_identity: true });
    expect(route.request().headers()['if-match']).toBe('"4"');
    if (calls === 1) { firstKey = route.request().headers()['idempotency-key']; return route.abort('failed'); }
    expect(route.request().headers()['idempotency-key']).toBe(firstKey);
    return route.fulfill(reply({ connection: { ...connection, revision: '5' }, replayed: true }));
  });
  await page.goto('/connections/cn_stdio');
  await page.getByRole('button', { name: 'Edit configuration' }).click();
  let dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Executable (full path)')).toHaveValue('');
  await expect(dialog.getByLabel('Arguments (one exact argument per line)')).toHaveValue('');
  await expect(dialog.getByLabel('Environment bindings (NAME=credential-reference per line)')).toHaveValue('');
  await dialog.getByRole('button', { name: 'Cancel' }).scrollIntoViewIfNeeded();
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(page.getByRole('button', { name: 'Edit configuration' })).toBeFocused();
  await page.getByRole('button', { name: 'Replace credential' }).click();
  dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Existing environment variable name')).toBeFocused();
  await dialog.getByLabel('Existing environment variable name').fill('TOKEN');
  await dialog.getByLabel('New secret').fill('synthetic-stdio-secret');
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Replace credential', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Retry exact replacement' })).toBeVisible();
  await expect(dialog.getByLabel('Existing environment variable name')).toBeDisabled();
  await dialog.getByRole('button', { name: 'Retry exact replacement' }).click();
  await expect(dialog).toHaveCount(0);
  expect(calls).toBe(2);
});

test('standard provider with default endpoint can author an explicit override', async ({ page }) => {
  await node(page);
  const connection = { id: 'cn_default', kind: 'model', display_name: 'Default endpoint', revision: '1',
    created_at: '2026-10-20T12:00:00Z', updated_at: '2026-10-20T12:00:00Z',
    spec: { shape: 'standard', provider_id: 'lmstudio', authentication_configured: false },
  };
  await page.route('**/api/v1/connections/cn_default', route => route.fulfill(reply(connection)));
  await page.route('**/api/v1/connections/cn_default/references', route => route.fulfill(reply({ connection_id: connection.id, connection_revision: '1', consumers: [] })));
  await page.goto('/connections/cn_default');
  await page.getByRole('button', { name: 'Edit configuration' }).click();
  await expect(page.getByRole('dialog').getByLabel('Endpoint (optional)')).toBeVisible();
});

test('Connection action growth preserves a readable heading across the shell breakpoint', async ({ page }, info) => {
  await node(page);
  await page.route('**/api/v1/connections/cn_heading', route => route.fulfill(reply({ id: 'cn_heading', kind: 'model', display_name: 'Lifecycle maintained', revision: '1',
    created_at: '2026-10-20T12:00:00Z', updated_at: '2026-10-20T12:00:00Z',
    spec: { shape: 'standard', provider_id: 'lmstudio', authentication_configured: true },
  })));
  await page.route('**/api/v1/connections/cn_heading/references', route => route.fulfill(reply({ connection_id: 'cn_heading', connection_revision: '1', consumers: [] })));
  await page.goto('/connections/cn_heading');
  const splitWords: number[] = [];
  for (const width of [390, 759, 760, 761, 1440]) {
    await page.setViewportSize({ width, height: 800 });
    await expect(page.getByRole('heading', { name: 'Lifecycle maintained' })).toBeVisible();
    await page.screenshot({ path: info.outputPath(`connection-heading-${width}.png`), fullPage: true });
    const intact = await page.getByRole('heading', { name: 'Lifecycle maintained' }).evaluate(heading => {
      const range = document.createRange();
      range.setStart(heading.firstChild!, 0);
      range.setEnd(heading.firstChild!, 'Lifecycle'.length);
      return range.getClientRects().length === 1;
    });
    if (!intact) splitWords.push(width);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  expect(splitWords).toEqual([]);
});

test('activation impact discloses broad grants and publication before enabling; cancel cannot mutate', async ({ page }) => {
  await node(page);
  const digest = 'a'.repeat(64);
  const review = { service: 'mcp', workspace_etag: 'gen-1', access_revision: 'grant-2', publication_revision: 'pub-3',
    affected_principals: [{ id: 'acc_build', label: 'Build agent' }, { id: 'acc_team', label: 'Team' }],
    published: true, requires_confirmation: true, digest, warning: 'Grant expansion' };
  let writes = 0;
  let activated = false;
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply(workspace(activated ? 'gen-2' : 'gen-1', activated ? ['models', 'mcp'] : ['models']))));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([
    { kind: 'models', enabled: true, configured: false },
    { kind: 'mcp', enabled: activated, configured: false, ...(!activated ? { activation_review: review } : {}) },
    { kind: 'search', enabled: false, configured: false },
  ])));
  await page.route('**/api/v1/workspaces/engineering/services/mcp', route => {
    writes++;
    expect(route.request().method()).toBe('PUT');
    expect(route.request().headers()['if-match']).toBe('"gen-1"');
    expect(route.request().postDataJSON()).toEqual({ review_digest: digest });
    activated = true;
    return route.fulfill(reply(workspace('gen-2', ['models', 'mcp'])));
  });
  await page.goto('/workspaces/engineering/services');
  const row = page.locator('.servicecontrol').filter({ has: page.getByText('MCP', { exact: true }) });
  await row.getByRole('button', { name: 'Enable' }).click();
  let dialog = page.getByRole('dialog', { name: 'Enable MCP' });
  await expect(dialog.getByText('Build agent')).toBeVisible();
  await expect(dialog.getByText('Team')).toBeVisible();
  await expect(dialog.getByText(/Published: this Service becomes addressable/)).toBeVisible();
  await expect.poll(() => writes).toBe(0);
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toHaveCount(0);
  expect(writes).toBe(0);
  await row.getByRole('button', { name: 'Enable' }).click();
  dialog = page.getByRole('dialog', { name: 'Enable MCP' });
  await dialog.getByRole('button', { name: 'Confirm enabling MCP' }).click();
  await expect(dialog).toHaveCount(0);
  expect(writes).toBe(1);
  await expect(row.getByRole('button', { name: 'Disable' })).toBeVisible();
});

test('stale activation review fails closed and offers a fresh decision', async ({ page }) => {
  await node(page);
  const review = { service: 'mcp', workspace_etag: 'gen-1', access_revision: 'grant-1', publication_revision: 'pub-1',
    affected_principals: [{ id: 'acc_build', label: 'Build agent' }], published: false, requires_confirmation: true,
    digest: 'b'.repeat(64) };
  let calls = 0;
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply(workspace())));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([
    { kind: 'models', enabled: true, configured: false }, { kind: 'mcp', enabled: false, configured: false, activation_review: review },
    { kind: 'search', enabled: false, configured: false },
  ])));
  await page.route('**/api/v1/workspaces/engineering/services/mcp', route => {
    calls++;
    return route.fulfill(reply({ type: 'about:blank', title: 'Changed', status: 409, code: 'REVISION_CONFLICT', detail: 'Grant review changed' }, 409));
  });
  await page.goto('/workspaces/engineering/services');
  await page.locator('.servicecontrol').filter({ has: page.getByText('MCP', { exact: true }) }).getByRole('button', { name: 'Enable' }).click();
  const dialog = page.getByRole('dialog', { name: 'Enable MCP' });
  await dialog.getByRole('button', { name: 'Confirm enabling MCP' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Grant review changed');
  await expect(dialog.getByRole('button', { name: 'Confirm enabling MCP' })).toBeDisabled();
  expect(calls).toBe(1);
  await dialog.getByRole('button', { name: 'Reload current review' }).click();
  await expect(dialog).toHaveCount(0);
  expect(calls).toBe(1);
});
