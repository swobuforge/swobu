import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import openapiTS, { astToString } from 'openapi-typescript';

// OpenAPI is the contract authority. Check mode never rewrites reviewed output.
const schema = new URL('../../api/openapi.json', import.meta.url);
const output = new URL('../src/api/generated/schema.ts', import.meta.url);
const generated = astToString(await openapiTS(schema));
if (process.argv.includes('--check')) {
  const current = await readFile(output, 'utf8');
  if (current !== generated) throw new Error('Generated API drift. Run bun run api:generate.');
} else {
  await mkdir(fileURLToPath(new URL('.', output)), { recursive: true });
  await writeFile(output, generated);
}
