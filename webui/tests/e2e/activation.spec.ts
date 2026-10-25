import { expect, test, type Page } from '@playwright/test';

// Test transport fixtures only; production never fabricates client or evidence state.
function reply(body: unknown, status = 200, headers: Record<string, string> = {}) {
  return { status, contentType: status >= 400 ? 'application/problem+json' : 'application/json',
    headers: { 'Cache-Control': 'no-store', ...headers }, body: JSON.stringify(body) };
}
const workspace = {
  slug: 'engineering', display_name: 'Engineering', management_etag: 'ws-7', management_revision: 7, routes: [{ name: 'coding', tiers: [{ targets: [{ id: 'openai', connection_id: 'cn_openai', model: 'gpt-5', protocol: 'responses' }] }] }],
  mcp_mounts: [], enabled_services: ['models', 'mcp', 'search'], readiness: { llm: 'ready', mcp: 'not_configured' },
};
const principal = 'acc_AAAAAAAAAAAAAAAAAAAAAA';
const authentication = { principal, permission_revision: 'permission-fixture', issue_credential: true };
async function node(page: Page) {
  await page.route('**/api/v1/workspaces/engineering/publications', route => route.fulfill(reply({ workspace: 'engineering', address: 'relay', published: false, revision: 'b'.repeat(64) })));
  await page.route('**/api/v1/workspaces/engineering/model-match?*', route => route.fulfill(reply({ matched: new URL(route.request().url()).searchParams.get('model') === 'coding', pattern: 'coding', priority: 1, tiers: 1, targets: 1 })));
  await page.route('**/api/v1/principals', route => route.fulfill(reply([{ id: principal, label: 'Local client', kind: 'managed', status: 'active', revision: 'principal-fixture', permissions: [{ workspace: 'engineering', action: 'use', services: ['models'] }] }])));
  await page.route('**/api/v1/node', route => route.fulfill(reply({
    revision: 'a'.repeat(64), configuration_mode: 'managed', workspace_count: 1, connection_count: 1,
    principal_count: 1, publication_count: 0, budget_policy_count: 0,
  })));
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply(workspace)));
  await page.route('**/api/v1/workspaces', route => route.fulfill(reply([{
    slug: 'engineering', display_name: 'Engineering', route_count: 1,
  }])));
}

test('Connect previews exactly before mutating a local client and distinguishes actual use', async ({ page }, testInfo) => {
  await node(page);
  await page.route('**/api/v1/clients?workspace=engineering', route => route.fulfill(reply([{ id: 'codex', name: 'Codex' }])));
  const plan = {
    authentication, revision: `connect-${'b'.repeat(64)}`, workspace: 'engineering', model_id: 'coding', client: { id: 'codex', name: 'Codex' },
    config_paths: ['/tmp/client/config.toml'], changes: [{ field: 'model_provider', before: 'old', before_exists: true, after: 'swobu' }],
    already_configured: false, requires_replace: true,
  };
  let previews = 0, applies = 0;
  await page.route('**/api/v1/client-configurations?dry_run=*', route => {
    const url = new URL(route.request().url());
    const body = route.request().postDataJSON();
    expect(route.request().method()).toBe('POST');
    expect(body.workspace).toBe('engineering');
    expect(body.model_id).toBe('coding');
    expect(body.client).toBe('codex');
    expect(body.principal).toBe(principal);
    if (url.searchParams.get('dry_run') === 'true') {
      previews++;
      expect(body.review).toBeUndefined();
      return route.fulfill(reply(plan, 200, { ETag: `"${plan.revision}"` }));
    }
    applies++;
    expect(body.review).toEqual({ authentication, revision: plan.revision, config_paths: plan.config_paths, changes: plan.changes });
    expect(route.request().headers()['if-match']).toBe(`"${plan.revision}"`);
    expect(route.request().headers()['idempotency-key']).toMatch(/^[a-f0-9-]{16,}$/);
    return route.fulfill(reply({ ...plan, authentication: { principal, permission_revision: 'permission-fixture', issue_credential: false, credential_id: 'acred_AAAAAAAAAAAAAAAAAAAAAA' }, changes: [], already_configured: true, requires_replace: false, replayed: false, revision: `connect-${'c'.repeat(64)}` }));
  });
  await page.goto('/workspaces/engineering/connect');
  await page.getByRole('combobox', { name: 'Installed application' }).selectOption('codex');
  await page.getByRole('combobox', { name: 'Principal', exact: true }).selectOption(principal);
  await page.getByLabel('Model ID', { exact: true }).fill('coding');
  await expect(page.getByText('Matched route:')).toBeVisible();
  expect((await page.getByLabel('Model ID', { exact: true }).boundingBox())?.height).toBeGreaterThanOrEqual(39);
  expect(applies).toBe(0);
  await page.getByRole('button', { name: 'Review configuration' }).click();
  await expect(page.getByRole('group', { name: 'Local configuration review' })).toContainText('model_provider');
  expect(previews).toBe(1);
  expect(applies).toBe(0);
  await page.getByLabel('Model ID', { exact: true }).fill('unconfigured');
  await expect(page.getByRole('group', { name: 'Local configuration review' })).toHaveCount(0);
  await expect(page.getByText('No route matches this model ID.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Review configuration' })).toHaveCount(0);
  await page.getByLabel('Model ID', { exact: true }).fill('coding');
  await page.getByRole('button', { name: 'Review configuration' }).click();
  await expect(page.getByRole('group', { name: 'Local configuration review' })).toBeVisible();
  expect(previews).toBe(2);
  for (const width of [390, 759, 760, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath(`connect-model-${width}.png`), fullPage: true });
  }
  await page.getByRole('button', { name: 'Apply reviewed configuration' }).click();
  await expect(page.getByText('Client configuration observed.')).toBeVisible();
  await expect(page.getByText(/run this application and inspect/)).toBeVisible();
  expect(applies).toBe(1);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('ambiguous Connect apply never silently retries or reports verified traffic', async ({ page }) => {
  await node(page);
  await page.route('**/api/v1/clients?workspace=engineering', route => route.fulfill(reply([{ id: 'codex', name: 'Codex' }])));
  let attempts = 0;
  await page.route('**/api/v1/client-configurations?dry_run=*', route => {
    const dry = new URL(route.request().url()).searchParams.get('dry_run');
    if (dry === 'true') return route.fulfill(reply({
      authentication, revision: `connect-${'b'.repeat(64)}`, workspace: 'engineering', model_id: 'coding', client: { id: 'codex', name: 'Codex' },
      config_paths: ['/tmp/config'], changes: [{ field: 'model', before: '', before_exists: false, after: 'coding' }],
      already_configured: false, requires_replace: false,
    }));
    attempts++;
    return route.fulfill(reply({ type: 'about:blank', title: 'Outcome unavailable', status: 503, code: 'UNAVAILABLE' }, 503));
  });
  await page.goto('/workspaces/engineering/connect');
  await page.getByRole('combobox', { name: 'Installed application' }).selectOption('codex');
  await page.getByRole('combobox', { name: 'Principal', exact: true }).selectOption(principal);
  await page.getByLabel('Model ID', { exact: true }).fill('coding');
  await expect(page.getByText('Matched route:')).toBeVisible();
  expect((await page.getByLabel('Model ID', { exact: true }).boundingBox())?.height).toBeGreaterThanOrEqual(39);
  await page.getByRole('button', { name: 'Review configuration' }).click();
  await page.getByRole('button', { name: 'Apply reviewed configuration' }).click();
  await expect(page.getByRole('alert')).toContainText('not acknowledged');
  await expect(page.getByRole('button', { name: 'Apply reviewed configuration' })).toBeDisabled();
  expect(attempts).toBe(1);
});

test('Activity renders retained Exchanges and exact causal detail with uncertain evidence', async ({ page }, testInfo) => {
  await node(page);
  await page.route('**/api/v1/activity/operations/ex_AAAAAAAAAAAAAAAAAAAAAA', route => route.fulfill(reply({
    kind: 'exchange', evidence: 'retained', exchange: {
      exchange_id: 'ex_AAAAAAAAAAAAAAAAAAAAAA', access_id: 'principal', workspace: 'engineering', started_at: '2026-10-18T12:02:00Z',
      delivery_result: 'client_write_failed', upstream_execution: { recorded_invocations: 1, occurred: 0, may_have_occurred: 1, did_not_occur: 0, unsettled: 0 }, invocations: [{ invocation_id: 'attempt_1', kind: 'model', started_at: '2026-10-18T12:02:01Z',
        provider: 'OpenAI', route: 'coding', model: 'gpt-5', outcome: 'failed', execution_possibility: 'may_have_occurred' }],
    },
  })));
  await page.route('**/api/v1/activity?*', route => {
    const u = new URL(route.request().url());
    expect(u.searchParams.get('workspace')).toBe('engineering');
    expect(u.searchParams.get('kind')).toBe('exchange');
    expect(u.searchParams.get('limit')).toBe('50');
    return route.fulfill(reply({
      scope: { kind: 'workspace', workspace: 'engineering' },
      exchanges: [{ exchange_id: 'ex_AAAAAAAAAAAAAAAAAAAAAA', access_id: 'principal', workspace: 'engineering',
        started_at: '2026-10-18T12:02:00Z', route: 'coding', provider: 'OpenAI', model: 'gpt-5',
        state: 'failed', attempt_count: 1 }], mcp_operations: [], page: { mode: 'retained', limit: 50 },
    }));
  });
  await page.goto('/activity?workspace=engineering&kind=exchange');
  await expect(page.getByRole('link', { name: /engineering · coding/ })).toBeVisible();
  await expect(page.getByText('Workspace-scoped reads do not certify Node-wide capture health.')).toBeVisible();
  await page.getByRole('link', { name: /engineering · coding/ }).click();
  await expect(page.getByRole('heading', { name: 'Upstream attempts and effects' })).toBeVisible();
  await expect(page.getByText('Upstream execution uncertain')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Inspect current route ↗' })).toHaveAttribute('href', '/workspaces/engineering/services/models/coding');
  for (const width of [390, 759, 761, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await expect(page.getByText('Upstream execution uncertain')).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath(`recovery-${width}.png`), fullPage: true });
  }
});
