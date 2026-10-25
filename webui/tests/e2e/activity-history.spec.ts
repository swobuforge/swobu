import { expect, test, type Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';

function reply(body: unknown) {
  return { status: 200, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) };
}
async function historyNode(page: Page) {
  await page.route('**/api/v1/node', route => route.fulfill(reply({ revision: 'a'.repeat(64), configuration_mode: 'managed', workspace_count: 1, connection_count: 1, principal_count: 1, publication_count: 0, budget_policy_count: 0 })));
  await page.route('**/api/v1/workspaces', route => route.fulfill(reply([{ slug: 'engineering', display_name: 'Engineering', route_count: 1 }])));
  await page.route('**/api/v1/principals', route => route.fulfill(reply([])));
  await page.route('**/api/v1/activity?**', route => {
    const url = new URL(route.request().url());
    return route.fulfill(reply({ scope: url.searchParams.get('workspace') ? { kind: 'workspace', workspace: url.searchParams.get('workspace') } : { kind: 'all' },
      exchanges: url.searchParams.get('kind') === 'mcp_operation' ? [] : [{ exchange_id: 'ex_AAAAAAAAAAAAAAAAAAAAAA', workspace: 'engineering', access_id: 'local', started_at: '2026-10-20T12:00:00Z', state: 'succeeded', route: 'coding', attempt_count: 1 }],
      ...(url.searchParams.has('limit') ? { page: { mode: 'retained', limit: 50, exchange_next: url.searchParams.get('kind') === 'exchange' && !url.searchParams.has('exchange_cursor') ? 'list-next' : undefined } } : {}) }));
  });
}

for (const width of [390, 759, 760, 761, 1440]) {
  test(`Activity history filters and export layout at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await historyNode(page);
    await page.goto('/activity?workspace=engineering&kind=exchange');
    await expect(page.getByRole('link', { name: /engineering · coding/ })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Settled from (inclusive)' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Download first page' })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('activity.png'), fullPage: true });
    const principal = page.getByRole('combobox', { name: 'Principal', exact: true });
    await principal.focus(); await expect(principal).toBeFocused();
    expect(await principal.evaluate(el => getComputedStyle(el).outlineStyle)).toBe('solid');
    await page.goto('/settings/history');
    await expect(page.getByRole('button', { name: 'Download first page' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link')).toHaveText(['History & exports', 'Network', 'Administration']);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('history.png'), fullPage: true });
  });
}

test('retained filters survive paging and detail navigation and clear for MCP/live', async ({ page }) => {
  await historyNode(page);
  const observed: URL[] = [];
  page.on('request', request => { if (new URL(request.url()).pathname === '/api/v1/activity') observed.push(new URL(request.url())); });
  await page.route('**/api/v1/activity/operations/*', route => route.fulfill(reply({ kind: 'exchange', evidence: 'retained', exchange: { exchange_id: 'ex_AAAAAAAAAAAAAAAAAAAAAA', workspace: 'engineering', access_id: 'local', started_at: '2026-10-20T12:00:00Z', upstream_execution: { recorded_invocations: 0, occurred: 0, may_have_occurred: 0, did_not_occur: 0, unsettled: 0 }, invocations: [] } })));
  await page.goto('/activity?workspace=engineering&kind=exchange&cursor=old-page');
  await page.getByRole('combobox', { name: 'Principal', exact: true }).fill('local');
  await page.getByRole('textbox', { name: 'Settled from (inclusive)' }).fill('2026-10-20T00:00:00Z');
  await page.getByRole('combobox', { name: 'Result', exact: true }).selectOption('succeeded');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page).toHaveURL(/principal=local/);
  expect(new URL(page.url()).searchParams.has('cursor')).toBe(false);
  await expect.poll(() => observed.at(-1)?.searchParams.get('principal')).toBe('local');
  await page.getByRole('button', { name: 'Older →' }).click();
  await expect.poll(() => observed.at(-1)?.searchParams.get('exchange_cursor')).toBe('list-next');
  expect(observed.at(-1)?.searchParams.get('from')).toBe('2026-10-20T00:00:00Z');
  expect(observed.at(-1)?.searchParams.get('result')).toBe('succeeded');
  await page.getByRole('link', { name: /engineering · coding/ }).click();
  await page.getByRole('link', { name: '← Activity', exact: true }).click();
  await expect(page).toHaveURL(/cursor=list-next/);
  await expect(page.getByRole('combobox', { name: 'Principal', exact: true })).toHaveValue('local');
  await page.getByRole('combobox', { name: 'Evidence', exact: true }).selectOption('mcp_operation');
  await expect.poll(() => observed.at(-1)?.searchParams.get('kind')).toBe('mcp_operation');
  for (const key of ['from', 'to', 'principal', 'result', 'exchange_cursor']) expect(observed.at(-1)?.searchParams.has(key)).toBe(false);
  await page.getByRole('combobox', { name: 'Evidence', exact: true }).selectOption('all');
  await expect(page.getByRole('button', { name: 'Download first page' })).toHaveCount(0);
});

test('History downloads MCP CSV and keeps Exchange-only filters out of its request', async ({ page }) => {
  await historyNode(page);
  let query: Record<string, string> = {};
  await page.route('**/api/v1/activity/export?**', route => {
    query = Object.fromEntries(new URL(route.request().url()).searchParams);
    return route.fulfill({ status: 200, contentType: 'text/csv; charset=utf-8', body: 'kind,tool\nmcp_operation,"catalog,lookup"\n', headers: {
      'Cache-Control': 'no-store', 'Content-Disposition': 'attachment; filename="swobu-activity-mcp_operation-engineering.csv"',
      'X-Swobu-Export-Mode': 'retained-page', 'X-Swobu-Export-Rows': '1',
    } });
  });
  await page.goto('/settings/history?workspace=engineering&kind=exchange&principal=local&result=succeeded');
  await page.getByRole('combobox', { name: 'Evidence', exact: true }).selectOption('mcp_operation');
  await expect(page.getByRole('button', { name: 'Apply filters' })).toHaveCount(0);
  await page.getByRole('combobox', { name: 'File format' }).selectOption('csv');
  await page.getByRole('spinbutton', { name: 'Rows per file' }).fill('1');
  const file = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download first page' }).click();
  const saved = await file;
  expect(saved.suggestedFilename()).toBe('swobu-activity-mcp_operation-engineering.csv');
  expect(await readFile((await saved.path())!, 'utf8')).toBe('kind,tool\nmcp_operation,"catalog,lookup"\n');
  expect(query).toEqual({ workspace: 'engineering', kind: 'mcp_operation', format: 'csv', max_rows: '1' });
});

test('empty retained history, invalid filters and oversized downloads remain explicit', async ({ page }) => {
  await historyNode(page);
  await page.route('**/api/v1/activity?**', route => {
    const query = new URL(route.request().url()).searchParams;
    if (query.has('from')) return route.fulfill({ status: 400, contentType: 'application/problem+json', body: JSON.stringify({ message: 'Activity query is invalid.' }) });
    return route.fulfill(reply({ scope: { kind: 'all' }, exchanges: [], page: { mode: 'retained', limit: 50 } }));
  });
  await page.route('**/api/v1/activity/export?**', route => route.fulfill({ status: 413, contentType: 'application/problem+json', body: '{}' }));
  await page.goto('/activity?kind=exchange');
  await expect(page.getByText('No evidence in this view', { exact: true })).toBeVisible();
  await page.getByRole('textbox', { name: 'Settled from (inclusive)' }).fill('not-a-time');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Settled from (inclusive)' })).toHaveValue('not-a-time');
  await page.getByRole('button', { name: 'Clear Exchange filters' }).click();
  await expect(page.getByText('No evidence in this view', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Download first page' }).click();
  await expect(page.getByRole('alert')).toContainText('Reduce rows per file');
});

test('downloaded pages retain filters, require explicit continuation and recover from failure', async ({ page }) => {
  await historyNode(page);
  const queries: Record<string, string>[] = [];
  let fail = true;
  await page.route('**/api/v1/activity/export?**', route => {
    const params = new URL(route.request().url()).searchParams;
    queries.push(Object.fromEntries(params));
    if (fail) return route.fulfill({ status: 503, contentType: 'application/problem+json', body: '{}' });
    return route.fulfill({ status: 200, contentType: 'application/x-ndjson; charset=utf-8', body: '{"kind":"exchange","exchange":{"exchange_id":"ex_file"}}\n', headers: {
      'Cache-Control': 'no-store', 'Content-Disposition': 'attachment; filename="swobu-activity-exchange-engineering.ndjson"',
      'X-Swobu-Export-Mode': 'retained-page', 'X-Swobu-Export-Rows': '1', ...(params.has('cursor') ? {} : { 'X-Swobu-Next-Cursor': 'export-next' }),
    } });
  });
  await page.goto('/activity?workspace=engineering&kind=exchange&principal=local&result=succeeded&cursor=list-page');
  await page.getByRole('button', { name: 'Download first page' }).click();
  await expect(page.getByRole('alert')).toContainText('No file was saved');
  expect(queries).toHaveLength(1);
  fail = false;
  const first = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download first page' }).click();
  const file = await first;
  expect(file.suggestedFilename()).toBe('swobu-activity-exchange-engineering.ndjson');
  expect(await readFile((await file.path())!, 'utf8')).toContain('ex_file');
  await expect(page.getByRole('button', { name: 'Download next page' })).toBeVisible();
  expect(queries).toHaveLength(2);
  const second = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download next page' }).click(); await second;
  expect(queries[2]).toEqual({ kind: 'exchange', format: 'ndjson', max_rows: '500', workspace: 'engineering', principal: 'local', result: 'succeeded', cursor: 'export-next' });
  expect(queries[1].cursor).toBeUndefined();
  await expect(page.getByRole('button', { name: 'Download next page' })).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText('capture may be incomplete');
});
