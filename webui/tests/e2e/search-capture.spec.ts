import { expect, test } from '@playwright/test';
for (const width of [390, 759, 760, 761, 1440]) {
  test(`Search responsive layout ${width}`, async ({ page }, info) => {
    await page.setViewportSize({width, height: 930});
    const json = (value: unknown) => ({ status: 200, contentType: 'application/json', headers: {'Cache-Control': 'no-store'}, body: JSON.stringify(value) });
    await page.route('**/api/v1/node', route => route.fulfill(json({revision: 'a'.repeat(64), configuration_mode:'managed', workspace_count:1})));
    await page.route('**/api/v1/workspaces/engineering', route => route.fulfill(json({slug:'engineering', display_name:'Engineering', management_etag:'g1-1', management_revision:1, routes:[], mcp_mounts:[], enabled_services:['search'], readiness:{search:'ready'}})));
    await page.route('**/api/v1/connections', route => route.fulfill(json([{id:'cn_primary', display_name:'Primary Search', kind:'search', revision:'1'}, {id:'cn_secondary', display_name:'Secondary Search', kind:'search', revision:'1'}])));
    await page.route('**/api/v1/workspaces/engineering/search-route', route => route.fulfill(json({connection_id:'cn_primary', fallback_connection_ids:[]})));
    await page.goto('/workspaces/engineering/services/search');
    await expect(page.getByRole('heading', {name:'Direct Search route'})).toBeVisible();
    await page.screenshot({path:info.outputPath('search.png'), fullPage:true});
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
}
