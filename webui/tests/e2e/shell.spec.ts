import { test, expect } from '@playwright/test';

for (const width of [390, 759, 760, 761, 1440]) {
  test(`navigation, finder and overflow at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/workspaces');
    const nav = page.getByRole('navigation', { name: 'Primary', exact: true });
    await expect(nav.getByRole('link')).toHaveText(['Workspaces', 'Connections', 'Access', 'Activity', 'Settings']);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Workspaces');
    await page.getByRole('button', { name: 'Find anything' }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await page.getByLabel('Find a page or resource').fill('connections');
    await page.getByRole('navigation', { name: 'Search results' }).getByRole('link', { name: 'Connections' }).click();
    await expect(page).toHaveURL(/\/connections$/);
    await expect(page.getByRole('dialog')).not.toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: `test-results/shell-${width}.png`, fullPage: true });
  });
}

test('direct Workspace navigation, reload and retired route rejection', async ({ page }) => {
  await page.route('**/api/v1/node', route => route.fulfill({ status: 200, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify({ revision: 'a'.repeat(64), configuration_mode: 'managed', workspace_count: 1, connection_count: 0, principal_count: 0, publication_count: 0, budget_policy_count: 0 }) }));
  await page.route('**/api/v1/workspaces/research', route => route.fulfill({ status: 200, contentType: 'application/json', headers: { 'Cache-Control': 'no-store', ETag: '"gen-1"' }, body: JSON.stringify({ slug: 'research', display_name: 'Research', management_etag: 'gen-1', management_revision: 1, routes: [], mcp_mounts: [], enabled_services: ['models'], readiness: { llm: 'not_configured', mcp: 'disabled' } }) }));
  await page.route('**/api/v1/workspaces/research/services', route => route.fulfill({ status: 200, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify([]) }));
  await page.goto('/workspaces/research/services/models');
  await expect(page.getByRole('navigation', { name: 'Workspace sections' }).getByRole('link')).toHaveText(['Overview', 'Services', 'Access', 'Limits', 'Connect']);
  await page.reload();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Research');
  // Vite's SPA preview serves the document for unknown paths; the typed router
  // still rejects the retired route instead of mounting a legacy surface.
  const retired = await page.request.get('/runtime');
  expect(retired.status()).toBe(200);
  expect(await retired.text()).not.toContain('/runtime');
});

test('keyboard finder closes and restores focus without persistent browser state', async ({ page }) => {
  await page.goto('/workspaces');
  const trigger = page.getByRole('button', { name: 'Find anything' });
  await trigger.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByLabel('Find a page or resource')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0]);
});
