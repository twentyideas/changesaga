import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { SiteAssetError, assertRendererShellOnly, findPrivateContent } from '../lib/site-assets';

const FIXTURE_SHELL = path.resolve(__dirname, 'fixtures', 'renderer-shell');
const REPO_ROOT = path.resolve(__dirname, '..', '..');

function tempDir(build: (root: string) => void): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'review-saga-shell-'));
  build(root);
  return root;
}

describe('renderer shell guard', () => {
  test('accepts the generic renderer shell fixture', () => {
    expect(findPrivateContent(FIXTURE_SHELL)).toEqual([]);
    expect(() => assertRendererShellOnly(FIXTURE_SHELL)).not.toThrow();
  });

  test("rejects this repository's real saga directory", () => {
    const sagaDir = path.join(REPO_ROOT, 'review-saga-v2.saga');
    // Guard the guard: the fixture is the checked-in saga for this repository.
    expect(fs.existsSync(sagaDir)).toBe(true);
    expect(() => assertRendererShellOnly(sagaDir)).toThrow(SiteAssetError);
  });

  test('rejects a shell directory with saga content copied into it', () => {
    const root = tempDir((dir) => {
      fs.writeFileSync(path.join(dir, 'index.html'), '<!doctype html>');
      fs.mkdirSync(path.join(dir, 'pr-1234.saga', 'overview.fragment'), { recursive: true });
      fs.writeFileSync(path.join(dir, 'pr-1234.saga', 'saga.json'), '{}');
    });
    expect(() => assertRendererShellOnly(root)).toThrow(/pr-1234\.saga/);
  });

  test.each([
    ['___diffs', 'private saga overlay directory'],
    ['___review', 'private saga overlay directory'],
    ['___approvals', 'private saga overlay directory'],
    ['___landmarks', 'private saga overlay directory'],
  ])('rejects the reserved %s directory', (name, reason) => {
    const root = tempDir((dir) => {
      fs.writeFileSync(path.join(dir, 'index.html'), '<!doctype html>');
      fs.mkdirSync(path.join(dir, 'nested', name), { recursive: true });
    });
    const violations = findPrivateContent(root);
    expect(violations).toHaveLength(1);
    expect(violations[0].relativePath).toBe(`nested/${name}`);
    expect(violations[0].reason).toContain(reason);
  });

  test.each(['saga.json', 'chapter.json', 'section.json', 'fragment.json', 'thread.json'])(
    'rejects the %s manifest anywhere in the tree',
    (name) => {
      const root = tempDir((dir) => {
        fs.writeFileSync(path.join(dir, 'index.html'), '<!doctype html>');
        fs.mkdirSync(path.join(dir, 'deep', 'nested'), { recursive: true });
        fs.writeFileSync(path.join(dir, 'deep', 'nested', name), '{}');
      });
      expect(() => assertRendererShellOnly(root)).toThrow(new RegExp(name.replace('.', '\\.')));
    },
  );

  test.each(['.env', '.env.production', 'github-app.pem', 'server.key', '.npmrc', 'id_rsa'])(
    'rejects the credential-like file %s',
    (name) => {
      const root = tempDir((dir) => {
        fs.writeFileSync(path.join(dir, 'index.html'), '<!doctype html>');
        fs.writeFileSync(path.join(dir, name), 'secret');
      });
      expect(() => assertRendererShellOnly(root)).toThrow(/credential or local-secret pattern/);
    },
  );

  test('rejects a checkout that carries .git or node_modules', () => {
    const root = tempDir((dir) => {
      fs.writeFileSync(path.join(dir, 'index.html'), '<!doctype html>');
      fs.mkdirSync(path.join(dir, '.git'));
      fs.mkdirSync(path.join(dir, 'node_modules'));
    });
    const violations = findPrivateContent(root);
    expect(violations.map((violation) => violation.relativePath).sort()).toEqual([
      '.git',
      'node_modules',
    ]);
  });

  test('requires an index.html entrypoint', () => {
    const root = tempDir((dir) => {
      fs.writeFileSync(path.join(dir, 'shell.js'), 'export default 1;');
    });
    expect(() => assertRendererShellOnly(root)).toThrow(/no index\.html/);
  });

  test('reports every violation, not just the first', () => {
    const root = tempDir((dir) => {
      fs.writeFileSync(path.join(dir, 'index.html'), '<!doctype html>');
      fs.writeFileSync(path.join(dir, '.env'), 'x');
      fs.mkdirSync(path.join(dir, 'a.chapter'));
      fs.mkdirSync(path.join(dir, 'b.fragment'));
    });
    expect(findPrivateContent(root)).toHaveLength(3);
  });
});
