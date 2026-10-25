import ts from 'typescript';
import { readdir, readFile, realpath } from 'node:fs/promises';
import { isAbsolute, relative, resolve, sep } from 'node:path';

/** Production source may depend only on production-local modules or installed
 * packages. Test/prototype data must never enter its transitive module graph.
 * Reject nonliteral imports because their target cannot be audited statically. */
export async function checkProductionSources(root: string): Promise<void> {
  const sourceRoot = await realpath(resolve(root, 'src'));
  async function visit(directory: string): Promise<void> {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const path = resolve(directory, entry.name);
      if (entry.isDirectory()) { await visit(path); continue; }
      if (!/\.[cm]?[jt]sx?$/.test(path) || /\.(test|spec)\.[jt]sx?$/.test(path)) continue;
      const raw = await readFile(path, 'utf8');
      const file = ts.createSourceFile(path, raw, ts.ScriptTarget.Latest, true);
      const specifiers: string[] = [];
      function inspect(node: ts.Node): void {
        if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier) {
          if (!ts.isStringLiteral(node.moduleSpecifier)) throw new Error(`Nonliteral module in ${path}`);
          specifiers.push(node.moduleSpecifier.text);
        }
        if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword || ts.isIdentifier(node.expression) && node.expression.text === 'require')) {
          if (node.arguments.length !== 1 || !ts.isStringLiteral(node.arguments[0])) throw new Error(`Nonliteral module in ${path}`);
          specifiers.push(node.arguments[0].text);
        }
        ts.forEachChild(node, inspect);
      }
      inspect(file);
      for (const specifier of specifiers) {
        const parts = specifier.replaceAll('\\', '/').split('/');
        if (parts.some(part => /^(?:__)?(?:tests?|fixtures?|mocks?|reference|prototype)(?:__)?$/i.test(part)) || /\.(test|spec)\.[jt]sx?$/.test(specifier)) {
          throw new Error(`Production import of test/fixture source: ${relative(root, path)} -> ${specifier}`);
        }
        if (specifier.startsWith('.') || isAbsolute(specifier)) {
          const target = resolve(directory, specifier);
          const local = relative(sourceRoot, target);
          if (local === '..' || local.startsWith(`..${sep}`) || isAbsolute(local)) throw new Error(`Production import escapes src: ${specifier}`);
        } else if (specifier.startsWith('@/') || specifier.startsWith('~/')) {
          throw new Error(`Unresolved source alias: ${specifier}`);
        }
      }
    }
  }
  await visit(sourceRoot);
}
