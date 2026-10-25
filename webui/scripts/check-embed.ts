import { cp, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

// Compile staged bytes as an embed.FS in isolation. W01 owns installing these
// bytes into the production package; this proof never writes that package.
const stage = resolve(import.meta.dir, '../.staging');
const temporary = await mkdtemp(join(tmpdir(), 'swobu-web-embed-'));
try {
  await cp(stage, join(temporary, 'assets'), { recursive: true });
  await writeFile(join(temporary, 'go.mod'), 'module stagedweb\n\ngo 1.25.0\n');
  await writeFile(join(temporary, 'assets_test.go'), `package stagedweb
import("embed";"encoding/json";"crypto/sha256";"fmt";"testing")
//go:embed assets/* assets/assets/*
var assets embed.FS
func TestStagedAssets(t *testing.T){
 raw,err:=assets.ReadFile("assets/manifest.json");if err!=nil {t.Fatal(err)}
 var manifest struct {Assets map[string]string};if err:=json.Unmarshal(raw,&manifest);err!=nil {t.Fatal(err)}
 if len(manifest.Assets)==0 {t.Fatal("empty manifest")}
 for name,want:=range manifest.Assets {raw,err:=assets.ReadFile("assets/"+name);if err!=nil {t.Fatal(err)};if got:=fmt.Sprintf("%x",sha256.Sum256(raw));got!=want {t.Fatalf("%s hash=%s want=%s",name,got,want)}}
}
`);
  const child = Bun.spawn(['go', 'test', '-mod=readonly', '-buildvcs=false', './...'], { cwd: temporary, env: { ...process.env, GOWORK: 'off' }, stdout: 'inherit', stderr: 'inherit' });
  if (await child.exited !== 0) throw new Error('Staged Go embed proof failed.');
  const manifest = JSON.parse(await readFile(join(stage, 'manifest.json'), 'utf8'));
  console.log(`Embedded ${Object.keys(manifest.assets).length} staged files; source ${manifest.source_sha256}`);
} finally {
  await rm(temporary, { recursive: true, force: true });
}
