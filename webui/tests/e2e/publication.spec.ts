import { expect, test, type Page } from '@playwright/test';

const revision = 'b'.repeat(64);
const changedRevision = 'c'.repeat(64);
const relayURL = 'https://node.example.test';
function reply(body: unknown, status = 200) {
  return { status, contentType: status >= 400 ? 'application/problem+json' : 'application/json',
    headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) };
}
async function setup(page: Page) {
  await page.route('**/api/v1/node', r => r.fulfill(reply({ revision, configuration_mode: 'managed', workspace_count: 1,
    principal_count: 0, connection_count: 0, publication_count: 0, budget_policy_count: 0 })));
  await page.route('**/api/v1/workspaces/engineering', r => r.fulfill(reply({ slug: 'engineering', display_name: 'Engineering',
    management_etag: 'ws-1', routes: [], enabled_services: ['models', 'mcp', 'search'], mcp_mounts: [] })));
  await page.route('**/api/v1/workspaces', r => r.fulfill(reply([{ slug: 'engineering', display_name: 'Engineering', route_count: 0 }])));
  await page.route('**/api/v1/principals', r => r.fulfill(reply([])));
  await page.route('**/api/v1/clients?*', r => r.fulfill(reply([])));
}

test('publish and unpublish use the observed revision and existing Workspace authority', async ({ page }) => {
  await setup(page);
  let published = false;
  const state = () => ({ workspace: 'engineering', address: 'relay', published, url: relayURL,
    revision: published ? changedRevision : revision });
  await page.route('**/api/v1/workspaces/engineering/publications', r => r.fulfill(reply(state())));
  const methods: string[] = [];
  await page.route('**/api/v1/workspaces/engineering/publications/relay', r => {
    const request = r.request();
    expect(request.headers()['if-match']).toBe(`"${state().revision}"`);
    expect(request.postData()).toBeNull();
    expect(request.headers()['idempotency-key']).toBeUndefined();
    methods.push(request.method());
    published = request.method() === 'PUT';
    return r.fulfill(reply(state()));
  });
  await page.goto('/workspaces/engineering/connect');
  await page.getByRole('button', { name: 'Publish via relay', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Unpublish', exact: true })).toBeVisible();
  await expect(page.getByText('Workspace credential required', { exact: true })).toBeVisible();
  for (const width of [390, 759, 760, 1440]) {
    await page.setViewportSize({ width, height: 930 });
    await expect(page.getByRole('button', { name: 'Unpublish', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: `test-results/publication-${width}.png`, fullPage: true });
  }
  await page.getByRole('button', { name: 'Unpublish', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Publish via relay', exact: true })).toBeVisible();
  expect(methods).toEqual(['PUT', 'DELETE']);
});

test('stale publication requires inspection before another change', async ({ page }) => {
  await setup(page);
  let inspected = false;
  const state = () => ({ workspace: 'engineering', address: 'relay', published: inspected, revision: inspected ? changedRevision : revision });
  await page.route('**/api/v1/workspaces/engineering/publications', r => r.fulfill(reply(state())));
  let attempts = 0;
  await page.route('**/api/v1/workspaces/engineering/publications/relay', r => {
    attempts++;
    return r.fulfill(reply({ type: 'about:blank', title: 'Publication changed', status: 412,
      detail: 'Inspect current state before another change.' }, 412));
  });
  await page.goto('/workspaces/engineering/connect');
  await page.getByRole('button', { name: 'Publish via relay', exact: true }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  expect(attempts).toBe(1);
  inspected = true;
  await page.getByRole('button', { name: 'Inspect current state', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Unpublish', exact: true })).toBeVisible();
  expect(attempts).toBe(1);
});

test('Network projects the real built-in relay address', async ({ page }) => {
  await setup(page);
  await page.route('**/api/v1/remote-addresses', r => r.fulfill(reply([{ id: 'relay', managed: true, url: relayURL }])));
  await page.goto('/settings/network');
  await expect(page.getByText(relayURL, { exact: true })).toBeVisible();
  await expect(page.getByText('Swobu manages one built-in relay address.', { exact: false })).toBeVisible();
});


test('published Workspace Connect presents exact remote endpoints and removes them on unpublish', async ({ page }, testInfo) => {
  await setup(page);
  let published = false;
  const state = () => ({ workspace: 'engineering', address: 'relay', published,
    url: relayURL, revision: published ? changedRevision : revision });
  await page.route('**/api/v1/workspaces/engineering/publications', r => r.fulfill(reply(state())));
  await page.route('**/api/v1/workspaces/engineering/publications/relay', r => {
    published = r.request().method() === 'PUT';
    return r.fulfill(reply(state()));
  });
  await page.goto('/workspaces/engineering/connect');
  const addresses = page.locator('[aria-label="Workspace connection addresses"]');
  const local = `${new URL(page.url()).origin}/workspaces/engineering`;
  await expect(addresses.getByText(`${local}/v1`, { exact: true })).toBeVisible();
  await expect(addresses.getByText(`${relayURL}/workspaces/engineering/v1`, { exact: true })).toHaveCount(0);
  await expect(page.getByText('This Workspace is not published.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Publish via relay' }).click();
  await expect(addresses.getByText(`${relayURL}/workspaces/engineering`, { exact: true })).toBeVisible();
  await expect(addresses.getByText(`${relayURL}/workspaces/engineering/v1`, { exact: true })).toBeVisible();
  await expect(addresses.getByText(`${relayURL}/workspaces/engineering/mcp`, { exact: true })).toBeVisible();
  await expect(addresses.getByText(`${relayURL}/workspaces/engineering/search`, { exact: true })).toBeVisible();
  // Both local and remote rows are addressable, each with its own copy action.
  await expect(page.getByRole('button', { name: 'Copy MCP endpoint URL' })).toHaveCount(2);
  for (const width of [390, 759, 760, 761, 1440]) {
    await page.setViewportSize({ width, height: 950 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath(`remote-connect-${width}.png`), fullPage: true });
  }
  await page.getByRole('button', { name: 'Unpublish' }).click();
  await expect(addresses.getByText(`${relayURL}/workspaces/engineering/v1`, { exact: true })).toHaveCount(0);
  await expect(addresses.getByText(`${local}/v1`, { exact: true })).toBeVisible();
});

test('Workspace Connect does not fabricate a remote address on absent or invalid publication state', async ({ page }) => {
  await setup(page);
  let origin = '';
  await page.route('**/api/v1/workspaces/engineering/publications', r => r.fulfill(reply({
    workspace: 'engineering', address: 'relay', published: true, url: origin, revision,
  })));
  await page.goto('/workspaces/engineering/connect');
  await expect(page.getByText('valid HTTPS relay address is not available', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Copy OpenAI-compatible API URL' })).toHaveCount(1);
  origin = 'http://node.example.test';
  await page.reload();
  await expect(page.getByText('valid HTTPS relay address is not available', { exact: false })).toBeVisible();
  expect(await page.getByText('http://node.example.test/workspaces/engineering/v1', { exact: true }).count()).toBe(0);
});

test('copying a remote endpoint includes no credential material', async ({ page }) => {
  await setup(page);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: (text: string) => { (window as Window & { __copiedURL?: string }).__copiedURL = text; return Promise.resolve(); } },
    });
  });
  await page.route('**/api/v1/workspaces/engineering/publications', r => r.fulfill(reply({
    workspace: 'engineering', address: 'relay', published: true, url: relayURL, revision,
  })));
  await page.goto('/workspaces/engineering/connect');
  const remoteRow = page.getByText(`${relayURL}/workspaces/engineering/mcp`, { exact: true }).locator('..');
  await remoteRow.getByRole('button', { name: 'Copy MCP endpoint URL' }).click();
  await expect(page.getByText('Endpoint URL copied.')).toBeVisible();
  expect(await page.evaluate(() => (window as Window & { __copiedURL?: string }).__copiedURL)).toBe(`${relayURL}/workspaces/engineering/mcp`);
});

test('long remote endpoints remain readable and keyboard-copy failure is announced', async ({ page }, testInfo) => {
  await setup(page);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true,
      value: { writeText: () => Promise.reject(new DOMException('Denied', 'NotAllowedError')) } });
  });
  const longOrigin = `https://${'workspace-'.repeat(6)}node.${'relay-'.repeat(10)}example.test`;
  await page.route('**/api/v1/workspaces/engineering/publications', r => r.fulfill(reply({
    workspace: 'engineering', address: 'relay', published: true, url: longOrigin, revision,
  })));
  await page.goto('/workspaces/engineering/connect');
  const endpoint = page.getByText(`${longOrigin}/workspaces/engineering/mcp`, { exact: true });
  const copy = endpoint.locator('..').getByRole('button', { name: 'Copy MCP endpoint URL', exact: true });
  for (const width of [390, 759, 760, 761, 1440]) {
    await page.setViewportSize({ width, height: 950 });
    await expect(endpoint).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath(`long-endpoint-${width}.png`), fullPage: true });
  }
  await copy.focus();
  await expect(copy).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('[aria-live="polite"]', { hasText: 'Clipboard access failed.' })).toBeVisible();
  await expect(endpoint).toBeVisible();
});

test('publication read failure hides remote addresses and allows explicit recovery', async ({ page }) => {
  await setup(page);
  let available = false;
  await page.route('**/api/v1/workspaces/engineering/publications', r => r.fulfill(available ? reply({
    workspace: 'engineering', address: 'relay', published: true, url: relayURL, revision,
  }) : reply({ title: 'Unavailable', status: 503 }, 503)));
  await page.goto('/workspaces/engineering/connect');
  const addresses = page.locator('[aria-label="Workspace connection addresses"]');
  await expect(addresses.getByRole('alert')).toContainText('Publication status unavailable');
  await expect(addresses.getByRole('button', { name: 'Copy MCP endpoint URL' })).toHaveCount(1);
  available = true;
  await addresses.getByRole('button', { name: 'Check again' }).click();
  await expect(addresses.getByText(`${relayURL}/workspaces/engineering/mcp`, { exact: true })).toBeVisible();
});
