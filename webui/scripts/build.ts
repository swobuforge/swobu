import { checkProductionSources } from './source-policy';
import { createHash } from 'node:crypto';
import { readdir, readFile, writeFile, mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, relative, resolve } from 'node:path';

const root = resolve(import.meta.dir, '..');
const check = process.argv.includes('--check');
async function run(args: string[]) {
  const child = Bun.spawn(args, { cwd: root, stdout: 'inherit', stderr: 'inherit' });
  if (await child.exited !== 0) throw new Error(`Failed: ${args.join(' ')}`);
}
async function files(directory: string): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true });
  const paths = await Promise.all(entries.map(entry => entry.isDirectory() ? files(join(directory, entry.name)) : [join(directory, entry.name)]));
  return paths.flat().sort();
}
const digest = (raw: Uint8Array | string) => createHash('sha256').update(raw).digest('hex');
await checkProductionSources(root);
await run(['bun', 'run', check ? 'api:check' : 'api:generate']);
await run(['bun', 'run', 'typecheck']);
const temporary = check ? await mkdtemp(join(tmpdir(), 'swobu-web-')) : undefined;
const output = temporary ? join(temporary, 'assets') : join(root, '.staging');
try {
  await run(['bun', 'x', '--no-install', 'vite', 'build', '--outDir', output]);
  const inputs = [...await files(join(root, 'src')), ...await files(join(root, 'scripts')), ...await files(join(root, 'public')),
    ...['index.html', 'package.json', 'bun.lock', 'tsconfig.json', 'vite.config.ts'].map(name => join(root, name)), resolve(root, '../api/openapi.json')].sort();
  const source = createHash('sha256');
  for (const path of inputs) {
    source.update(relative(root, path)); source.update('\0'); source.update(await readFile(path)); source.update('\0');
  }
  const assets: Record<string, string> = {};
  for (const path of await files(output)) {
    const raw = await readFile(path);
    if (/\.(js|html|css)$/.test(path)) {
      const text = raw.toString();
      if (['/_swobu', '/c/', 'fixture-secret', 'fixture-workspace'].some(marker => text.includes(marker))) throw new Error(`Forbidden fixture/legacy marker in ${path}`);
    }
    assets[relative(output, path)] = digest(raw);
  }
  await writeFile(join(output, 'manifest.json'), JSON.stringify({ source_sha256: source.digest('hex'), assets }, null, 2) + '\n');
  if (check) {
    const expected = join(root, '.staging');
    const names = (await files(output)).map(path => relative(output, path));
    const current = (await files(expected)).map(path => relative(expected, path));
    if (JSON.stringify(names) !== JSON.stringify(current)) throw new Error('Staged asset file drift. Run make webui-generate.');
    for (const name of names) if (digest(await readFile(join(output, name))) !== digest(await readFile(join(expected, name)))) throw new Error(`Staged asset drift: ${name}`);
  }
} finally {
  if (temporary) await rm(temporary, { recursive: true, force: true });
}
