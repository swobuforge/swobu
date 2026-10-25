import { test, expect } from '@playwright/test';
import { PNG } from 'pngjs';
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

// The accepted prototype is read only by browser proof, never by production code.
const referencePath = resolve(import.meta.dirname, '../../../../..', 'docs/00-inbox/Swobu_Pivot_Closeout_War_Room_2026-10-15/reference/Swobu_Operator_Ergonomics_v5.html');
for (const width of [390, 1440]) {
  test(`accepted shell comparison at ${width}px`, async ({ page, context }) => {
    await page.setViewportSize({ width, height: 930 });
    await page.goto('/connections');
    const reference = await context.newPage();
    await reference.setViewportSize({ width, height: 930 });
    await reference.goto('http://127.0.0.1:4178');
    await reference.setContent(await readFile(referencePath, 'utf8'));
    await reference.evaluate(() => { location.hash = '#connections'; });
    await expect(reference.locator('.nav a[aria-current="page"]')).toHaveText('Connections');
    const selectors = ['.rail', '.brand', '.nav', '.top', '.content', '.findbtn'];
    for (const selector of selectors) {
      const actual = await page.locator(selector).boundingBox();
      const accepted = await reference.locator(selector).boundingBox();
      expect(actual, selector).not.toBeNull();
      expect(accepted, selector).not.toBeNull();
      // Content height depends on real resource state; all shell registration is fixed.
      for (const key of ['x', 'y', 'width'] as const) expect(Math.abs(actual![key] - accepted![key]), `${selector}.${key}`).toBeLessThan(1);
    }
    const actualPNG = PNG.sync.read(await page.screenshot({ path: `test-results/reproduce-${width}-actual.png`, fullPage: false }));
    const acceptedPNG = PNG.sync.read(await reference.screenshot({ path: `test-results/reproduce-${width}-reference.png`, fullPage: false }));
    const diff = new PNG({ width: actualPNG.width, height: actualPNG.height });
    // Full-frame differences remain visible: resource content and Node facts
    // deliberately differ from the reference's fixture state during B01.
    for (let index = 0; index < actualPNG.data.length; index += 4) {
      const changed = actualPNG.data.subarray(index, index + 3).some((value, channel) => value !== acceptedPNG.data[index + channel]);
      diff.data.set(changed ? [220, 30, 70, 255] : [245, 245, 245, 255], index);
    }
    await writeFile(`test-results/reproduce-${width}-diff.png`, PNG.sync.write(diff));
    await reference.close();
  });
}
