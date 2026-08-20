/**
 * Minimal health/config handler behind the HTTP API boundary.
 *
 * This exists so that later work — GitHub App authentication, comment posting,
 * private saga rendering — has a place to land that is already wired through
 * CloudFront, throttled, logged, and running under a least-privilege role.
 *
 * It deliberately returns no secrets. Secret material is referenced by ARN in
 * the environment and reported only as a boolean "configured" flag.
 */

/** Structural subset of the API Gateway HTTP API (payload format 2.0) event. */
interface HttpApiEvent {
  readonly version?: string;
  readonly rawPath?: string;
  readonly requestContext?: {
    readonly requestId?: string;
    readonly http?: { readonly method?: string; readonly path?: string };
  };
}

interface HttpApiResult {
  statusCode: number;
  headers: Record<string, string>;
  body: string;
}

/** Feature flags consumed by the renderer shell. All are off in this iteration. */
interface FeatureFlags {
  /** GitHub App credentials are configured for this deployment. */
  readonly githubApp: boolean;
  /** Comment posting back to GitHub is implemented and enabled. */
  readonly commentPosting: boolean;
  /** Rendering of sagas from private repositories is implemented and enabled. */
  readonly privateRepositories: boolean;
}

const BASE_HEADERS: Record<string, string> = {
  'content-type': 'application/json; charset=utf-8',
  'cache-control': 'no-store',
  'x-content-type-options': 'nosniff',
};

function env(name: string): string | undefined {
  const value = process.env[name];
  return value === undefined || value.trim() === '' ? undefined : value.trim();
}

function features(): FeatureFlags {
  return {
    githubApp: env('GITHUB_APP_SECRET_ARN') !== undefined && env('GITHUB_APP_ID') !== undefined,
    commentPosting: false,
    privateRepositories: false,
  };
}

function json(
  statusCode: number,
  body: unknown,
  extraHeaders: Record<string, string> = {},
): HttpApiResult {
  return {
    statusCode,
    headers: { ...BASE_HEADERS, ...extraHeaders },
    body: JSON.stringify(body),
  };
}

function health(requestId: string | undefined): HttpApiResult {
  return json(200, {
    status: 'ok',
    service: env('APP_NAME') ?? 'review-saga',
    environment: env('APP_ENVIRONMENT') ?? 'unknown',
    version: env('APP_VERSION') ?? '0.0.0-dev',
    time: new Date().toISOString(),
    requestId: requestId ?? null,
  });
}

/**
 * Public, non-secret runtime configuration for the renderer shell.
 *
 * The shell is a static, cacheable artifact, so it cannot be built per
 * environment. It fetches this document at startup instead.
 */
function config(): HttpApiResult {
  return json(200, {
    environment: env('APP_ENVIRONMENT') ?? 'unknown',
    version: env('APP_VERSION') ?? '0.0.0-dev',
    siteUrl: env('SITE_URL') ?? null,
    apiBasePath: '/api',
    features: features(),
    /**
     * Where the renderer should fetch saga content from. `none` means this
     * deployment serves the shell only; private content still requires an
     * authenticated content endpoint, which is not part of this iteration.
     */
    sagaSource: 'none',
  });
}

const ALLOWED: Record<string, { method: string; handle: (requestId?: string) => HttpApiResult }> = {
  '/api/health': { method: 'GET', handle: (requestId) => health(requestId) },
  '/api/config': { method: 'GET', handle: () => config() },
};

export async function handler(event: HttpApiEvent): Promise<HttpApiResult> {
  const requestContext = event.requestContext ?? {};
  const method = (requestContext.http?.method ?? 'GET').toUpperCase();
  const rawPath = event.rawPath ?? requestContext.http?.path ?? '/';
  // Normalise a trailing slash so /api/health and /api/health/ behave alike.
  const routePath = rawPath.length > 1 ? rawPath.replace(/\/+$/, '') : rawPath;

  const route = ALLOWED[routePath];
  if (route === undefined) {
    return json(404, { error: 'not_found', path: routePath });
  }
  if (route.method !== method) {
    return json(405, { error: 'method_not_allowed', allow: route.method }, { allow: route.method });
  }

  try {
    return route.handle(requestContext.requestId);
  } catch (error) {
    // Never leak internals to the caller; the detail goes to CloudWatch Logs.
    console.error('unhandled error', { path: routePath, error });
    return json(500, { error: 'internal_error' });
  }
}
