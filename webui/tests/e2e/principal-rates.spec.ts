import { expect, test, type Page } from '@playwright/test';

const revision = 'b'.repeat(64);
const before = { workspace: 'engineering', action: 'use', services: ['models', 'mcp'], request_limits: [{ service: 'models', requests_per_minute: 60 }] };
function reply(body: unknown, status = 200) {
  return { status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) };
}
async function fixture(page: Page, granted = true) {
  await page.route('**/api/v1/node', route => route.fulfill(reply({ revision, configuration_mode: 'managed', workspace_count: 1, principal_count: 1, connection_count: 0, publication_count: 0, budget_policy_count: 0 })));
  await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(reply({ slug: 'engineering', display_name: 'Engineering', management_etag: revision, management_revision: 1, routes: [], mcp_mounts: [], enabled_services: ['models', 'mcp'], readiness: { llm: 'not_configured', mcp: 'not_configured' } })));
  await page.route('**/api/v1/workspaces/engineering/services', route => route.fulfill(reply([{ kind: 'models', enabled: true }, { kind: 'mcp', enabled: true }, { kind: 'search', enabled: false }])));
  await page.route('**/api/v1/principals', route => route.fulfill(reply([{ id: 'pr_alice', label: 'Alice', kind: 'managed', status: 'active', revision, permissions: granted ? [before] : [] }])));
  await page.goto('/workspaces/engineering/access');
  if (granted) await page.getByRole('button', { name: 'Edit Services' }).click();
  else {
    await page.getByRole('button', { name: 'Grant access', exact: true }).click();
    await page.getByRole('button', { name: /Alice/ }).click();
  }
}

for (const width of [390, 759, 761, 1440]) {
  test(`review and commit exact Principal rates at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 950 });
    let applies = 0;
    const after = { ...before, request_limits: [{ service: 'models', requests_per_minute: 12 }, { service: 'mcp', requests_per_minute: 3 }] };
    await page.route('**/api/v1/workspaces/engineering/permissions/pr_alice*', route => {
      expect(route.request().method()).toBe('PUT');
      expect(route.request().postDataJSON()).toEqual({ action: 'use', services: after.services, request_limits: after.request_limits });
      if (new URL(route.request().url()).searchParams.get('dry_run') === 'true') {
        return route.fulfill(reply({ principal: 'pr_alice', workspace: 'engineering', operation: 'grant', revision, before, after, affected_credentials: 2, warning: 'Preview is not approval.' }));
      }
      applies++;
      expect(route.request().headers()['if-match']).toBe(`"${revision}"`);
      expect(route.request().headers()['idempotency-key']).toBeTruthy();
      return route.fulfill(reply({ principal: 'pr_alice', workspace: 'engineering', revision, permission: after, allowed: true, replayed: false }));
    });
    await fixture(page);
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByRole('spinbutton', { name: 'Models requests / minute' })).toHaveValue('60');
    await dialog.getByRole('spinbutton', { name: 'Models requests / minute' }).fill('0');
    await expect(dialog.getByRole('button', { name: 'Review grant' })).toBeDisabled();
    await dialog.getByRole('spinbutton', { name: 'Models requests / minute' }).fill('1.5');
    await expect(dialog.getByRole('button', { name: 'Review grant' })).toBeDisabled();
    await dialog.getByRole('spinbutton', { name: 'Models requests / minute' }).fill('100001');
    await expect(dialog.getByRole('button', { name: 'Review grant' })).toBeDisabled();
    await dialog.getByRole('spinbutton', { name: 'Models requests / minute' }).fill('12');
    await dialog.getByRole('spinbutton', { name: 'MCP requests / minute' }).fill('3');
    await page.screenshot({ path: info.outputPath('rates-edit.png'), fullPage: true });
    await dialog.getByRole('button', { name: 'Review grant' }).click();
    await expect(dialog).toContainText('Models: 60 req/min');
    await expect(dialog).toContainText('Models: 12 req/min · MCP: 3 req/min');
    expect(applies).toBe(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('rates-review.png'), fullPage: true });
    await dialog.getByRole('button', { name: 'Confirm grant' }).click();
    await expect(dialog).toHaveCount(0);
    expect(applies).toBe(1);
  });
}

test('new grant sends explicit empty ceilings; stale apply requires fresh review', async ({ page }) => {
  let previews = 0, applies = 0;
  await page.route('**/api/v1/workspaces/engineering/permissions/pr_alice*', route => {
    expect(route.request().postDataJSON()).toEqual({ action: 'use', services: ['models'], request_limits: [] });
    if (new URL(route.request().url()).searchParams.get('dry_run') === 'true') {
      previews++;
      return route.fulfill(reply({ principal: 'pr_alice', workspace: 'engineering', operation: 'grant', revision, before: null, after: { workspace: 'engineering', action: 'use', services: ['models'] }, affected_credentials: 0, warning: 'Preview is not approval.' }));
    }
    applies++;
    return route.fulfill(reply({ title: 'Changed', status: 412, code: 'REVISION_CONFLICT' }, 412));
  });
  await fixture(page, false);
  await expect(page.getByRole('spinbutton', { name: 'MCP requests / minute' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Review grant' }).click();
  await expect(page.getByRole('dialog')).toContainText('No per-Principal request limit');
  await page.getByRole('button', { name: 'Confirm grant' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  expect(applies).toBe(1);
  await page.getByRole('button', { name: 'Inspect current state' }).click();
  await expect(page.getByRole('button', { name: 'Review grant' })).toBeVisible();
  expect(applies).toBe(1);
  await page.getByRole('button', { name: 'Review grant' }).click();
  expect(previews).toBe(2);
});
