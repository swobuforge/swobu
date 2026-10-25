import { createHash } from 'node:crypto';
import { cp, mkdir, readFile, readdir, rename, rm, stat } from 'node:fs/promises';
import { join, resolve } from 'node:path';

// Run only after "bun scripts/build.ts --check" and check-embed.ts.
// Install one verified Vite output into the Go embed package. No parallel
// source-less UI exists after cutover; the Go binary gets only this output.
const source = resolve(import.meta.dir, '../.staging');
const destination = resolve(import.meta.dir, '../../internal/operatorweb/assets');
const parent = resolve(destination, '..');
const manifest = JSON.parse(await readFile(join(source, 'manifest.json'), 'utf8')) as { source_sha256: string; assets: Record<string, string> };
if (!/^[a-f0-9]{64}$/.test(manifest.source_sha256)) throw new Error('Invalid staged source manifest');
const digest = (raw: Uint8Array) => createHash('sha256').update(raw).digest('hex');
async function files(dir: string, prefix = ''): Promise<string[]> {
  const output: string[] = [];
  for (const item of await readdir(dir, { withFileTypes: true })) {
    const name = prefix ? `${prefix}/${item.name}` : item.name;
    if (item.isDirectory()) output.push(...await files(join(dir, item.name), name));
    else if (item.isFile()) output.push(name);
    else throw new Error(`Refusing non-file asset ${name}`);
  }
  return output.sort();
}
const expected = Object.keys(manifest.assets).sort();
const actual = (await files(source)).filter(name => name !== 'manifest.json');
if (JSON.stringify(actual) !== JSON.stringify(expected)) throw new Error('Staged asset manifest does not match its files');
for (const name of expected) {
  if (name.includes('..') || name.startsWith('/')) throw new Error(`Unsafe asset path ${name}`);
  const raw = await readFile(join(source, name));
  if (digest(raw) !== manifest.assets[name]) throw new Error(`Staged asset differs from its manifest: ${name}`);
}
if (!expected.includes('index.html') || !expected.includes('swobu.png') || !expected.some(name => name.startsWith('assets/') && name.endsWith('.js'))) throw new Error('Incomplete Operator build');
const candidate = join(parent, `.incoming-web-${process.pid}`);
const backup = join(parent, `.prior-web-${process.pid}`);
await rm(candidate, { recursive: true, force: true });
await cp(source, candidate, { recursive: true });
await mkdir(parent, { recursive: true });
let moved = false;
try {
  try { await stat(destination); await rename(destination, backup); moved = true; } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
  }
  await rename(candidate, destination);
} catch (error) {
  if (moved) await rename(backup, destination);
  throw error;
} finally {
  await rm(candidate, { recursive: true, force: true });
}
if (moved) await rm(backup, { recursive: true, force: true });
console.log(`Installed ${expected.length} verified Operator assets into Go embed; source ${manifest.source_sha256}`);
