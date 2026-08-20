import * as fs from 'fs';
import * as path from 'path';

/**
 * Configuration for the Review Saga hosting stack.
 *
 * Every value is resolved from CDK context (`-c key=value`, `cdk.json`, or
 * `cdk.context.json`) with an environment-variable fallback for the deployment
 * target. Nothing here is a credential: secrets are referenced by ARN and read
 * at runtime by the API handler.
 */
export interface HostingConfig {
  /** Slug used to prefix every physical resource name. */
  readonly appName: string;
  /** Deployment environment slug, e.g. `dev`, `staging`, `prod`. */
  readonly environmentName: string;
  /** Version string surfaced by `GET /api/config`; never a secret. */
  readonly appVersion: string;
  /** Deployment target. Both fields may be undefined for an environment-agnostic stack. */
  readonly account?: string;
  readonly region?: string;
  /** Optional custom domain. Absent means "use the CloudFront domain name". */
  readonly domain?: DomainConfig;
  /** Optional local directory holding the generic renderer shell to upload. */
  readonly siteAssetPath?: string;
  /** Optional Secrets Manager ARN holding GitHub App credentials (placeholder for later work). */
  readonly githubAppSecretArn?: string;
  /** Optional non-secret GitHub App id, surfaced only as a "configured" boolean. */
  readonly githubAppId?: string;
  /** CloudWatch Logs retention, in days. */
  readonly logRetentionDays: number;
  /** Whether to provision a CloudFront access-log bucket. */
  readonly enableAccessLogs: boolean;
  /** Whether buckets survive `cdk destroy`. Defaults to true for `prod`. */
  readonly retainData: boolean;
  /** CloudFront price class. */
  readonly priceClass: PriceClassName;
  /** HTTP API default-stage throttling. */
  readonly apiThrottle: ThrottleConfig;
}

export interface DomainConfig {
  /** Primary domain, e.g. `saga.example.com`. */
  readonly domainName: string;
  /** Additional CloudFront aliases covered by the same certificate. */
  readonly alternativeNames: string[];
  /** ACM certificate ARN. CloudFront requires the certificate to live in us-east-1. */
  readonly certificateArn: string;
  /** Route 53 hosted zone to write alias records into. Both fields are required together. */
  readonly hostedZoneId?: string;
  readonly hostedZoneName?: string;
}

export interface ThrottleConfig {
  readonly rateLimit: number;
  readonly burstLimit: number;
}

export type PriceClassName = 'PriceClass_100' | 'PriceClass_200' | 'PriceClass_All';

/** Raised for any invalid or inconsistent context value. */
export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigError';
  }
}

/** Minimal read-only view of `Node.tryGetContext`, so this module stays testable without a `Stack`. */
export interface ContextReader {
  tryGetContext(key: string): unknown;
}

export const CONTEXT_PREFIX = 'reviewSaga';

export const CONTEXT_KEYS = {
  appName: `${CONTEXT_PREFIX}:appName`,
  environment: `${CONTEXT_PREFIX}:environment`,
  appVersion: `${CONTEXT_PREFIX}:appVersion`,
  account: `${CONTEXT_PREFIX}:account`,
  region: `${CONTEXT_PREFIX}:region`,
  domainName: `${CONTEXT_PREFIX}:domainName`,
  alternativeDomainNames: `${CONTEXT_PREFIX}:alternativeDomainNames`,
  certificateArn: `${CONTEXT_PREFIX}:certificateArn`,
  hostedZoneId: `${CONTEXT_PREFIX}:hostedZoneId`,
  hostedZoneName: `${CONTEXT_PREFIX}:hostedZoneName`,
  siteAssetPath: `${CONTEXT_PREFIX}:siteAssetPath`,
  githubAppSecretArn: `${CONTEXT_PREFIX}:githubAppSecretArn`,
  githubAppId: `${CONTEXT_PREFIX}:githubAppId`,
  logRetentionDays: `${CONTEXT_PREFIX}:logRetentionDays`,
  enableAccessLogs: `${CONTEXT_PREFIX}:enableAccessLogs`,
  retainData: `${CONTEXT_PREFIX}:retainData`,
  priceClass: `${CONTEXT_PREFIX}:priceClass`,
  apiRateLimit: `${CONTEXT_PREFIX}:apiRateLimit`,
  apiBurstLimit: `${CONTEXT_PREFIX}:apiBurstLimit`,
} as const;

const SLUG = /^[a-z][a-z0-9-]*$/;
const HOSTNAME = /^(?!-)[a-z0-9-]{1,63}(?<!-)(\.(?!-)[a-z0-9-]{1,63}(?<!-))+$/;
const ACCOUNT = /^\d{12}$/;
const REGION = /^[a-z]{2}(-gov)?-[a-z]+-\d$/;
const ACM_US_EAST_1 = /^arn:aws[a-z-]*:acm:us-east-1:\d{12}:certificate\/[0-9a-f-]+$/;
const SECRET_ARN = /^arn:aws[a-z-]*:secretsmanager:[a-z0-9-]+:\d{12}:secret:.+$/;
const HOSTED_ZONE_ID = /^[A-Z0-9]{1,32}$/;

/** Retention values supported by `logs.RetentionDays`. */
export const ALLOWED_LOG_RETENTION_DAYS = [
  1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288,
  3653,
];

const PRICE_CLASSES: PriceClassName[] = ['PriceClass_100', 'PriceClass_200', 'PriceClass_All'];

/**
 * Reads and validates every supported context key.
 *
 * @param node    context source, normally `app.node`
 * @param options `projectRoot` anchors relative paths; `env` supplies the
 *                environment-variable fallback (defaults to `process.env`).
 */
export function resolveConfig(
  node: ContextReader,
  options: { projectRoot: string; env?: NodeJS.ProcessEnv } = { projectRoot: process.cwd() },
): HostingConfig {
  const env = options.env ?? process.env;
  const read = (key: string): string | undefined => {
    const raw = node.tryGetContext(key);
    if (raw === undefined || raw === null) {
      return undefined;
    }
    if (typeof raw === 'string') {
      const trimmed = raw.trim();
      return trimmed === '' ? undefined : trimmed;
    }
    if (typeof raw === 'number' || typeof raw === 'boolean') {
      return String(raw);
    }
    if (Array.isArray(raw)) {
      return raw.join(',');
    }
    throw new ConfigError(`Context "${key}" must be a string, number, boolean, or array.`);
  };

  const appName = read(CONTEXT_KEYS.appName) ?? 'review-saga';
  requireMatch(appName, SLUG, CONTEXT_KEYS.appName, 'lowercase letters, digits, and hyphens');
  requireMaxLength(appName, 32, CONTEXT_KEYS.appName);

  const environmentName = read(CONTEXT_KEYS.environment) ?? 'dev';
  requireMatch(
    environmentName,
    SLUG,
    CONTEXT_KEYS.environment,
    'lowercase letters, digits, and hyphens',
  );
  requireMaxLength(environmentName, 16, CONTEXT_KEYS.environment);

  const appVersion = read(CONTEXT_KEYS.appVersion) ?? '0.0.0-dev';
  requireMaxLength(appVersion, 64, CONTEXT_KEYS.appVersion);

  const account = read(CONTEXT_KEYS.account) ?? env.CDK_DEFAULT_ACCOUNT ?? undefined;
  if (account !== undefined) {
    requireMatch(account, ACCOUNT, CONTEXT_KEYS.account, 'a 12-digit AWS account id');
  }

  const region = read(CONTEXT_KEYS.region) ?? env.CDK_DEFAULT_REGION ?? undefined;
  if (region !== undefined) {
    requireMatch(region, REGION, CONTEXT_KEYS.region, 'an AWS region such as us-east-1');
  }

  const domain = resolveDomain(read);

  const siteAssetPath = resolveSiteAssetPath(read(CONTEXT_KEYS.siteAssetPath), options.projectRoot);

  const githubAppSecretArn = read(CONTEXT_KEYS.githubAppSecretArn);
  if (githubAppSecretArn !== undefined) {
    requireMatch(
      githubAppSecretArn,
      SECRET_ARN,
      CONTEXT_KEYS.githubAppSecretArn,
      'a Secrets Manager secret ARN',
    );
  }

  const githubAppId = read(CONTEXT_KEYS.githubAppId);
  if (githubAppId !== undefined && !/^\d{1,20}$/.test(githubAppId)) {
    throw new ConfigError(`Context "${CONTEXT_KEYS.githubAppId}" must be a numeric GitHub App id.`);
  }

  const logRetentionDays = parseNumber(
    read(CONTEXT_KEYS.logRetentionDays),
    90,
    CONTEXT_KEYS.logRetentionDays,
  );
  if (!ALLOWED_LOG_RETENTION_DAYS.includes(logRetentionDays)) {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.logRetentionDays}" must be one of ${ALLOWED_LOG_RETENTION_DAYS.join(', ')}.`,
    );
  }

  const enableAccessLogs = parseBoolean(
    read(CONTEXT_KEYS.enableAccessLogs),
    true,
    CONTEXT_KEYS.enableAccessLogs,
  );

  const retainData = parseBoolean(
    read(CONTEXT_KEYS.retainData),
    environmentName === 'prod',
    CONTEXT_KEYS.retainData,
  );

  const priceClass = (read(CONTEXT_KEYS.priceClass) ?? 'PriceClass_100') as PriceClassName;
  if (!PRICE_CLASSES.includes(priceClass)) {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.priceClass}" must be one of ${PRICE_CLASSES.join(', ')}.`,
    );
  }

  const rateLimit = parseNumber(read(CONTEXT_KEYS.apiRateLimit), 25, CONTEXT_KEYS.apiRateLimit);
  const burstLimit = parseNumber(read(CONTEXT_KEYS.apiBurstLimit), 50, CONTEXT_KEYS.apiBurstLimit);
  if (rateLimit <= 0 || burstLimit <= 0) {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.apiRateLimit}" and "${CONTEXT_KEYS.apiBurstLimit}" must be positive numbers.`,
    );
  }
  if (burstLimit < rateLimit) {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.apiBurstLimit}" (${burstLimit}) must be greater than or equal to "${CONTEXT_KEYS.apiRateLimit}" (${rateLimit}).`,
    );
  }

  return {
    appName,
    environmentName,
    appVersion,
    ...(account !== undefined ? { account } : {}),
    ...(region !== undefined ? { region } : {}),
    ...(domain !== undefined ? { domain } : {}),
    ...(siteAssetPath !== undefined ? { siteAssetPath } : {}),
    ...(githubAppSecretArn !== undefined ? { githubAppSecretArn } : {}),
    ...(githubAppId !== undefined ? { githubAppId } : {}),
    logRetentionDays,
    enableAccessLogs,
    retainData,
    priceClass,
    apiThrottle: { rateLimit, burstLimit },
  };
}

/** Stable, human-readable prefix for physical resource names. */
export function resourcePrefix(config: HostingConfig): string {
  return `${config.appName}-${config.environmentName}`;
}

function resolveDomain(read: (key: string) => string | undefined): DomainConfig | undefined {
  const domainName = read(CONTEXT_KEYS.domainName);
  const certificateArn = read(CONTEXT_KEYS.certificateArn);
  const hostedZoneId = read(CONTEXT_KEYS.hostedZoneId);
  const hostedZoneName = read(CONTEXT_KEYS.hostedZoneName);
  const alternativeNames = splitList(read(CONTEXT_KEYS.alternativeDomainNames));

  if (domainName === undefined) {
    for (const [key, value] of [
      [CONTEXT_KEYS.certificateArn, certificateArn],
      [CONTEXT_KEYS.hostedZoneId, hostedZoneId],
      [CONTEXT_KEYS.hostedZoneName, hostedZoneName],
      [CONTEXT_KEYS.alternativeDomainNames, alternativeNames.length > 0 ? 'set' : undefined],
    ] as const) {
      if (value !== undefined) {
        throw new ConfigError(
          `Context "${key}" requires "${CONTEXT_KEYS.domainName}" to be set as well.`,
        );
      }
    }
    return undefined;
  }

  requireMatch(domainName, HOSTNAME, CONTEXT_KEYS.domainName, 'a fully qualified domain name');
  for (const name of alternativeNames) {
    requireMatch(
      name,
      HOSTNAME,
      CONTEXT_KEYS.alternativeDomainNames,
      'a comma-separated list of fully qualified domain names',
    );
  }

  if (certificateArn === undefined) {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.certificateArn}" is required when "${CONTEXT_KEYS.domainName}" is set. ` +
        'CloudFront only accepts ACM certificates issued in us-east-1.',
    );
  }
  if (!ACM_US_EAST_1.test(certificateArn)) {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.certificateArn}" must be an ACM certificate ARN in us-east-1 (CloudFront requirement).`,
    );
  }

  if ((hostedZoneId === undefined) !== (hostedZoneName === undefined)) {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.hostedZoneId}" and "${CONTEXT_KEYS.hostedZoneName}" must be provided together, ` +
        'or omitted together to manage DNS outside this stack.',
    );
  }
  if (hostedZoneId !== undefined) {
    requireMatch(
      hostedZoneId,
      HOSTED_ZONE_ID,
      CONTEXT_KEYS.hostedZoneId,
      'a Route 53 hosted zone id',
    );
    requireMatch(hostedZoneName!, HOSTNAME, CONTEXT_KEYS.hostedZoneName, 'a hosted zone name');
    const zone = hostedZoneName!.replace(/\.$/, '');
    for (const name of [domainName, ...alternativeNames]) {
      if (name !== zone && !name.endsWith(`.${zone}`)) {
        throw new ConfigError(
          `Domain "${name}" is not inside hosted zone "${zone}"; remove "${CONTEXT_KEYS.hostedZoneId}" and create the DNS record manually.`,
        );
      }
    }
  }

  return {
    domainName,
    alternativeNames,
    certificateArn,
    ...(hostedZoneId !== undefined ? { hostedZoneId } : {}),
    ...(hostedZoneName !== undefined ? { hostedZoneName } : {}),
  };
}

function resolveSiteAssetPath(value: string | undefined, projectRoot: string): string | undefined {
  if (value === undefined) {
    return undefined;
  }
  const resolved = path.isAbsolute(value) ? value : path.resolve(projectRoot, value);
  let stat: fs.Stats;
  try {
    stat = fs.statSync(resolved);
  } catch {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.siteAssetPath}" points at "${resolved}", which does not exist.`,
    );
  }
  if (!stat.isDirectory()) {
    throw new ConfigError(
      `Context "${CONTEXT_KEYS.siteAssetPath}" must be a directory; "${resolved}" is not.`,
    );
  }
  return resolved;
}

function splitList(value: string | undefined): string[] {
  if (value === undefined) {
    return [];
  }
  return value
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry !== '');
}

function parseNumber(value: string | undefined, fallback: number, key: string): number {
  if (value === undefined) {
    return fallback;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) {
    throw new ConfigError(`Context "${key}" must be an integer, got "${value}".`);
  }
  return parsed;
}

function parseBoolean(value: string | undefined, fallback: boolean, key: string): boolean {
  if (value === undefined) {
    return fallback;
  }
  if (value === 'true' || value === '1') {
    return true;
  }
  if (value === 'false' || value === '0') {
    return false;
  }
  throw new ConfigError(`Context "${key}" must be "true" or "false", got "${value}".`);
}

function requireMatch(value: string, pattern: RegExp, key: string, expectation: string): void {
  if (!pattern.test(value)) {
    throw new ConfigError(`Context "${key}" must be ${expectation}, got "${value}".`);
  }
}

function requireMaxLength(value: string, max: number, key: string): void {
  if (value.length > max) {
    throw new ConfigError(
      `Context "${key}" must be at most ${max} characters, got ${value.length}.`,
    );
  }
}
