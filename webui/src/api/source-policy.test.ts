import { expect, it } from 'vitest';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checkProductionSources } from '../../scripts/source-policy';

it('rejects fixture imports, reexports, dynamic modules and source escapes', async () => {
  const root = await mkdtemp(join(tmpdir(), 'swobu-source-policy-'));
  try {
    await mkdir(join(root, 'src'));
    for (const source of [
      "import data from '../tests/data';",
      "export { data } from './fixtures/data';",
      "const data = import('./mocks/data');",
      "const data = require('../reference/prototype');",
      "const data = import(target);",
      "import data from '../outside';",
    ]) {
      await writeFile(join(root, 'src/app.ts'), source);
      await expect(checkProductionSources(root)).rejects.toThrow();
    }
    await writeFile(join(root, 'src/app.ts'), "import React from 'react'; export { value } from './production';");
    await expect(checkProductionSources(root)).resolves.toBeUndefined();
  } finally { await rm(root, { recursive: true, force: true }); }
});
