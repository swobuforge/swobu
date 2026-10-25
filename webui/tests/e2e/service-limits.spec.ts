import { expect, test, type Page } from '@playwright/test';

const unlimited = () => ({ models: { requests_per_minute: 0, concurrent: 0 }, mcp: { requests_per_minute: 0 }, search: { requests_per_minute: 0 } });
function reply(body: unknown, status = 200, headers: Record<string, string> = {}) {
  return { status, contentType: status >= 400 ? 'application/problem+json' : 'application/json', headers: { 'Cache-Control': 'no-store', ...headers }, body: JSON.stringify(body) };
}
async function limitsNode(page: Page, mode = 'managed', enabled = ['models', 'mcp', 'search']) {
  await page.route('**/api/v1/node', route => route.fulfill(reply({ revision: 'a'.repeat(64), configuration_mode: mode, workspace_count: 1, connection_count: 0, principal_count: 0, publication_count: 0, budget_policy_count: 0 })));
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply({ slug: 'engineering', display_name: 'Engineering', management_etag: 'gen-1', enabled_services: enabled, routes: [], mcp_mounts: [], readiness: { llm: 'not_configured', mcp: 'not_configured', search: 'not_configured' } })));
  await page.route('**/api/v1/workspaces/engineering/limits/status', route => route.fulfill(reply({ workspace: 'engineering', limit: null, window_key: '2026-10-21', reserved_cents: 0, settled_cents: 0, uncertain_cents: 0, charged_cents: 0, remaining_cents: null, unknown_price_operations: 0, partial_quantity_operations: 0, unquoted_operations: 0 }, 200, { ETag: `"${'a'.repeat(64)}"` })));
}
for (const width of [390, 759, 760, 761, 1440]) {
  test(`Workspace operational limits at ${width}px`, async ({ page }, info) => {
    await limitsNode(page);
    await page.route('**/api/v1/workspaces/engineering/service-limits', route => route.fulfill(reply(unlimited(), 200, { ETag: '"gen-1"' })));
    await page.setViewportSize({ width, height: 950 });
    await page.goto('/workspaces/engineering/limits');
    await expect(page.getByRole('heading', { name: 'Workspace limits', exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Spending policy', exact: true })).toBeVisible();
    await expect(page.getByRole('combobox')).toHaveCount(4);
    const rate = page.getByRole('combobox', { name: 'Requests / minute policy' });
    await rate.focus(); await expect(rate).toBeFocused();
    expect(await rate.evaluate(el => getComputedStyle(el).outlineStyle)).toBe('solid');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('limits.png'), fullPage: true });
  });
}

test('save uses the reviewed revision, returns policy and preserves independent spending', async ({ page }) => {
  await limitsNode(page);
  let policy = unlimited(); let revision = 'gen-1'; let writes = 0;
  await page.route('**/api/v1/workspaces/engineering/service-limits', route => {
    if (route.request().method() === 'PUT') {
      writes++;
      expect(route.request().headers()['if-match']).toBe('"gen-1"');
      expect(route.request().headers()['idempotency-key']).toBeUndefined();
      policy = route.request().postDataJSON(); revision = 'gen-2';
    }
    return route.fulfill(reply(policy, 200, { ETag: `"${revision}"` }));
  });
  await page.goto('/workspaces/engineering/limits');
  await page.getByRole('combobox', { name: 'Requests / minute policy' }).selectOption('limited');
  await page.getByRole('spinbutton', { name: 'Requests / minute', exact: true }).fill('12');
  await page.getByRole('combobox', { name: 'Concurrent exchanges policy' }).selectOption('limited');
  await page.getByRole('spinbutton', { name: 'Concurrent exchanges', exact: true }).fill('2');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Workspace limits saved.');
  expect(writes).toBe(1); expect(policy.models).toEqual({ requests_per_minute: 12, concurrent: 2 });
  await expect(page.getByText('No spending ceiling')).toBeVisible();
  await page.reload();
  await expect(page.getByRole('spinbutton', { name: 'Requests / minute', exact: true })).toHaveValue('12');
});

for (const status of [412, 503]) {
  test(`failed save ${status} locks until explicit current-state read`, async ({ page }) => {
    await limitsNode(page); let writes = 0; let reads = 0;
    await page.route('**/api/v1/workspaces/engineering/service-limits', route => {
      if (route.request().method() === 'PUT') { writes++; return route.fulfill(reply({ title: 'Limits changed', status, code: status === 412 ? 'PRECONDITION_FAILED' : 'UNAVAILABLE' }, status)); }
      reads++; return route.fulfill(reply(unlimited(), 200, { ETag: `"gen-${reads}"` }));
    });
    await page.goto('/workspaces/engineering/limits');
    await page.getByRole('combobox', { name: 'Tool calls / minute policy' }).selectOption('limited');
    await page.getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('read current state');
    await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
    await expect(page.getByRole('combobox', { name: 'Tool calls / minute policy' })).toBeDisabled();
    expect(writes).toBe(1);
    await page.getByRole('button', { name: 'Read current limits' }).click();
    await expect(page.getByRole('combobox', { name: 'Tool calls / minute policy' })).toBeEnabled();
    await expect(page.getByRole('combobox', { name: 'Tool calls / minute policy' })).toHaveValue('unlimited');
    expect(reads).toBe(2); expect(writes).toBe(1);
  });
}

test('invalid ceilings cannot save and externally managed configuration cannot edit', async ({ page }) => {
  await limitsNode(page);
  let writes = 0;
  await page.route('**/api/v1/workspaces/engineering/service-limits', route => { if (route.request().method() === 'PUT') writes++; return route.fulfill(reply(unlimited(), 200, { ETag: '"gen-1"' })); });
  await page.goto('/workspaces/engineering/limits');
  await page.getByRole('combobox', { name: 'Concurrent exchanges policy' }).selectOption('limited');
  const concurrent = page.getByRole('spinbutton', { name: 'Concurrent exchanges', exact: true });
  await concurrent.fill('0'); await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await concurrent.fill('10001'); await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await concurrent.fill(''); await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Discard changes' }).click();
  await expect(concurrent).toHaveCount(0); expect(writes).toBe(0);
  await limitsNode(page, 'read_only', ['models']);
  await page.reload();
  await expect(page.getByRole('combobox', { name: 'Requests / minute policy' })).toBeDisabled();
  await expect(page.getByRole('combobox', { name: 'Tool calls / minute policy' })).toHaveCount(0);
});
