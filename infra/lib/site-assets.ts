import * as fs from 'fs';
import * as path from 'path';

/**
 * Guard rails for the public static origin.
 *
 * The S3 bucket behind CloudFront holds the *generic renderer shell* only:
 * HTML, JavaScript, CSS, fonts, and icons that are identical for every viewer.
 * Saga narratives, review threads, approvals, and source diffs are private
 * per-repository data. They must never be staged into this bucket, because
 * every object there is readable by anyone who can reach the distribution.
 *
 * `assertRendererShellOnly` runs at synthesis time, so an accidental
 * `cdk deploy -c reviewSaga:siteAssetPath=../pr-1234.saga` fails before any
 * object is uploaded.
 */

/** Directory suffixes that identify Review Saga content packages. */
const SAGA_DIR_SUFFIXES = ['.saga', '.chapter', '.fragment', '.landmark', '.thread', '.message'];

/** Reserved overlay directories used by the saga format. */
const SAGA_RESERVED_DIRS = ['___diffs', '___review', '___approvals', '___landmarks'];

/** Manifest files that only ever appear inside private saga content. */
const SAGA_MANIFEST_FILES = [
  'saga.json',
  'chapter.json',
  'section.json',
  'fragment.json',
  'thread.json',
  'landmark.json',
  'review-event.json',
];

/** Files that are secrets or local state and never belong on a public origin. */
const SECRET_FILE_PATTERNS: RegExp[] = [
  /^\.env(\..+)?$/i,
  /^\.npmrc$/i,
  /^\.netrc$/i,
  /^id_(rsa|dsa|ecdsa|ed25519)$/i,
  /\.pem$/i,
  /\.p12$/i,
  /\.pfx$/i,
  /\.key$/i,
  /^credentials$/i,
  /^\.git-credentials$/i,
];

/** Directories that are development or version-control state, not shell assets. */
const EXCLUDED_DIRS = ['.git', '.aws', '.ssh', 'node_modules'];

/** Maximum number of entries walked before giving up, as a runaway guard. */
const MAX_ENTRIES = 20000;

export class SiteAssetError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'SiteAssetError';
  }
}

/** A single reason a path was rejected. */
export interface AssetViolation {
  /** Path relative to the asset root. */
  readonly relativePath: string;
  readonly reason: string;
}

/**
 * Returns every reason `root` is unsuitable as the public static origin.
 * An empty array means the directory looks like a generic renderer shell.
 */
export function findPrivateContent(root: string): AssetViolation[] {
  const violations: AssetViolation[] = [];
  let visited = 0;

  const walk = (dir: string, relative: string): void => {
    let entries: fs.Dirent[];
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch (error) {
      throw new SiteAssetError(`Cannot read site asset directory "${dir}": ${String(error)}`);
    }
    for (const entry of entries) {
      if (++visited > MAX_ENTRIES) {
        throw new SiteAssetError(
          `Site asset directory "${root}" contains more than ${MAX_ENTRIES} entries; ` +
            'this looks like a repository checkout rather than a renderer shell.',
        );
      }
      const childRelative = relative === '' ? entry.name : `${relative}/${entry.name}`;
      if (entry.isDirectory()) {
        const suffix = SAGA_DIR_SUFFIXES.find((candidate) => entry.name.endsWith(candidate));
        if (suffix !== undefined) {
          violations.push({
            relativePath: childRelative,
            reason: `"${suffix}" directories hold private saga content`,
          });
          continue;
        }
        if (SAGA_RESERVED_DIRS.includes(entry.name)) {
          violations.push({
            relativePath: childRelative,
            reason: `"${entry.name}" is a private saga overlay directory`,
          });
          continue;
        }
        if (EXCLUDED_DIRS.includes(entry.name)) {
          violations.push({
            relativePath: childRelative,
            reason: `"${entry.name}" is local development state, not a shell asset`,
          });
          continue;
        }
        walk(path.join(dir, entry.name), childRelative);
        continue;
      }
      if (!entry.isFile()) {
        continue;
      }
      if (SAGA_MANIFEST_FILES.includes(entry.name)) {
        violations.push({
          relativePath: childRelative,
          reason: `"${entry.name}" is a saga manifest and describes private review content`,
        });
        continue;
      }
      if (SECRET_FILE_PATTERNS.some((pattern) => pattern.test(entry.name))) {
        violations.push({
          relativePath: childRelative,
          reason: 'file name matches a credential or local-secret pattern',
        });
      }
    }
  };

  walk(root, '');
  return violations;
}

/**
 * Throws unless `root` looks like a generic renderer shell: it must contain an
 * `index.html` entrypoint and no private saga content or credential-like files.
 */
export function assertRendererShellOnly(root: string): void {
  // Private content is checked first: it is the more dangerous mistake, and its
  // message is more useful than "no index.html" when both apply.
  const violations = findPrivateContent(root);
  if (violations.length > 0) {
    throw privateContentError(root, violations);
  }

  if (!fs.existsSync(path.join(root, 'index.html'))) {
    throw new SiteAssetError(
      `Site asset directory "${root}" has no index.html. The public origin holds the generic ` +
        'renderer shell; point reviewSaga:siteAssetPath at the built shell, not at saga content.',
    );
  }
}

function privateContentError(root: string, violations: AssetViolation[]): SiteAssetError {
  const detail = violations
    .slice(0, 20)
    .map((violation) => `  - ${violation.relativePath}: ${violation.reason}`)
    .join('\n');
  const overflow =
    violations.length > 20 ? `\n  ... and ${violations.length - 20} more entries` : '';
  return new SiteAssetError(
    `Refusing to publish "${root}" to the public static bucket.\n` +
      `Only the generic renderer shell belongs there; private saga and code content must be ` +
      `served through an authenticated path.\n${detail}${overflow}`,
  );
}
