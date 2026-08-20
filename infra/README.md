# Review Saga hosting (AWS CDK v2)

Infrastructure for **shareable Review Saga sites**: a public entrypoint that
serves the generic renderer shell, plus a secured API boundary where GitHub App
authentication, private saga content, and comment posting will be added.

This iteration provisions the foundation only. It deliberately does **not**
implement GitHub OAuth, comment posting, or private saga delivery; it gives
those a place to land that is already routed, throttled, logged, and running
under least-privilege IAM. See [Next steps](#next-steps).

## Architecture

```text
                     ┌──────────────────────────── CloudFront ────────────────────────────┐
                     │  TLS, compression, security response headers, cache separation      │
  viewer ─── https ──▶                                                                     │
                     │  default  ─────────────────▶ S3 (private, Origin Access Control)    │
                     │           /assets/*  ──────▶ S3, immutable 1-year cache             │
                     │           /api/*     ──────▶ HTTP API ─▶ Lambda (health/config)     │
                     └────────────────────────────────────────────────────────────────────┘
```

| Concern        | Resource                             | Notes                                                               |
| -------------- | ------------------------------------ | ------------------------------------------------------------------- |
| Renderer shell | Private S3 bucket                    | Block Public Access on, SSE-S3, TLS-only bucket policy, versioned   |
| Edge           | CloudFront distribution              | HTTP/2 + HTTP/3, IPv6, redirect-to-HTTPS, standard access logs      |
| S3 access      | Origin Access Control (OAC)          | SigV4; the bucket policy allows only _this_ distribution            |
| API boundary   | API Gateway HTTP API + Lambda        | `GET /api/health`, `GET /api/config`; throttled; JSON access logs   |
| URL rewriting  | CloudFront Function (viewer request) | Directory URLs → `index.html`, attached to the shell behaviour only |
| Secrets        | Referenced by ARN only               | No credential is ever written into the template                     |

Everything lives in one stack, `<appName>-<environment>-hosting`
(default `review-saga-dev-hosting`), defined in [`lib/hosting-stack.ts`](lib/hosting-stack.ts).

### Why the API sits behind CloudFront

`/api/*` is routed through the same distribution as the shell so that the
browser sees a **single origin**. That keeps the Content-Security-Policy at
`default-src 'self'`, removes the need for CORS, and means a future session
cookie can be `SameSite=Strict` and host-only. The API behaviour uses the
managed `CACHING_DISABLED` cache policy and `ALL_VIEWER_EXCEPT_HOST_HEADER`
origin request policy, so requests are forwarded intact and nothing is cached.

### Why there are no CloudFront custom error responses

The usual SPA trick — map 403/404 to `/index.html` with status 200 — is applied
distribution-wide, so it would also rewrite genuine `404`s and `403`s coming
back from `/api/*`. Instead, a small CloudFront Function rewrites
directory-style URLs, and it is attached only to the shell behaviour. A test
asserts that `CustomErrorResponses` stays absent.

### Cache separation

| Path                | Policy                      | TTL                    | Rationale                                   |
| ------------------- | --------------------------- | ---------------------- | ------------------------------------------- |
| default (`/`, HTML) | `<prefix>-shell`            | 5 min default, 1 h max | shell releases roll out quickly             |
| `/assets/*`         | `<prefix>-immutable-assets` | 1 year, min = max      | content-hashed build output                 |
| `/api/*`            | managed `CACHING_DISABLED`  | none                   | per-viewer by construction once auth exists |

Build the shell so that everything under `/assets/` carries a content hash in
its filename. Then a deploy only needs to invalidate `/` and `/index.html`,
which is what the optional bucket deployment does.

## Prerequisites

- Node.js 22+ and npm 10+ (`node --version`).
- An AWS account, and credentials for it in your shell (`aws sts get-caller-identity`).
- The CDK CLI is installed locally as a dev dependency — use `npm run …`, or
  `npx cdk …`; a global install is not required.
- For a custom domain: an ACM certificate **in us-east-1** (CloudFront only
  accepts certificates from that region), and optionally a Route 53 hosted zone.

Nothing here needs Docker: the Lambda is bundled with esbuild.

`infra/go.mod` is not a Go project. It exists so that the repository's
`go build ./...`, `go vet ./...`, and `go test ./...` skip this directory —
without it, the Go source that `npm install` leaves under
`infra/node_modules/aws-cdk/lib/init-templates/` makes those commands fail.

## Commands

All commands run from `infra/`.

```sh
npm install          # once
npm run build        # type-check the infrastructure and the Lambda handler
npm test             # CDK assertion tests + Lambda handler unit tests (no AWS calls)
npm run synth        # render the CloudFormation template into cdk.out/
npm run format       # prettier
npm run check        # format:check + build + test + synth, i.e. the whole gate
```

Deployment:

```sh
npm run bootstrap    # once per account/region: cdk bootstrap
npm run diff         # review the change set
npm run deploy       # cdk deploy
npm run destroy      # cdk destroy
```

`npm test` and `npm run synth` never contact AWS and need no credentials.
`bootstrap`, `diff`, `deploy`, and `destroy` do.

Pass context with `-c`, e.g.:

```sh
npm run deploy -- -c reviewSaga:environment=prod -c reviewSaga:priceClass=PriceClass_200
```

## Configuration

Configuration comes from CDK context, with an environment-variable fallback for
the deployment target. Every value is validated at synthesis time by
[`lib/config.ts`](lib/config.ts), so a typo fails before anything is created.

| Context key                         | Default                         | Meaning                                         |
| ----------------------------------- | ------------------------------- | ----------------------------------------------- |
| `reviewSaga:appName`                | `review-saga`                   | Prefix for physical resource names              |
| `reviewSaga:environment`            | `dev`                           | Environment slug; also drives `retainData`      |
| `reviewSaga:appVersion`             | `0.0.0-dev`                     | Reported by `/api/health`; not a secret         |
| `reviewSaga:account`                | `$CDK_DEFAULT_ACCOUNT`          | Target account (12 digits)                      |
| `reviewSaga:region`                 | `$CDK_DEFAULT_REGION`           | Target region                                   |
| `reviewSaga:domainName`             | _(none)_                        | Custom domain; see below                        |
| `reviewSaga:alternativeDomainNames` | _(none)_                        | Comma-separated extra aliases                   |
| `reviewSaga:certificateArn`         | _(none)_                        | ACM certificate ARN, **must be us-east-1**      |
| `reviewSaga:hostedZoneId`           | _(none)_                        | Route 53 zone for automatic alias records       |
| `reviewSaga:hostedZoneName`         | _(none)_                        | Required together with the zone id              |
| `reviewSaga:siteAssetPath`          | _(none)_                        | Local renderer-shell directory to upload        |
| `reviewSaga:githubAppSecretArn`     | _(none)_                        | Secrets Manager ARN, placeholder for later work |
| `reviewSaga:githubAppId`            | _(none)_                        | Non-secret numeric GitHub App id                |
| `reviewSaga:logRetentionDays`       | `90`                            | CloudWatch retention and access-log expiry      |
| `reviewSaga:enableAccessLogs`       | `true`                          | Provision the CloudFront access-log bucket      |
| `reviewSaga:retainData`             | `true` for `prod`, else `false` | Keep buckets on `cdk destroy`                   |
| `reviewSaga:priceClass`             | `PriceClass_100`                | `PriceClass_100`/`_200`/`_All`                  |
| `reviewSaga:apiRateLimit`           | `25`                            | HTTP API steady-state requests/second           |
| `reviewSaga:apiBurstLimit`          | `50`                            | HTTP API burst capacity                         |

Validation covers, among others: slug shapes, account and region formats,
certificate region, hosted-zone/domain consistency, throttle ordering
(`burst >= rate`), retention values CloudWatch actually accepts, and the
existence of `siteAssetPath`.

### Custom domain (documented, not hard-coded)

No domain is baked into the stack. Without one, the site is served at the
CloudFront domain printed as the `SiteUrl` output.

To use your own domain:

```sh
npm run deploy -- \
  -c reviewSaga:environment=prod \
  -c reviewSaga:domainName=saga.example.com \
  -c reviewSaga:certificateArn=arn:aws:acm:us-east-1:111122223333:certificate/<id>
```

That adds the alias and the certificate but touches no DNS — point a `CNAME`
(or an alias record in your own zone) at the `DistributionDomainName` output.

To have the stack manage DNS as well, add the hosted zone. Both keys are
required together, and every alias must live inside that zone:

```sh
  -c reviewSaga:hostedZoneId=Z0123456789ABCDEFGHIJ \
  -c reviewSaga:hostedZoneName=example.com
```

A and AAAA alias records are then created for the domain and every alternative
name.

### Publishing the renderer shell

Two options.

**Let CDK upload it** — point at the built shell directory:

```sh
npm run deploy -- -c reviewSaga:siteAssetPath=../path/to/built-shell
```

This adds a bucket deployment that prunes removed files and invalidates `/`
and `/index.html`.

**Or upload it yourself**, using the stack outputs:

```sh
aws s3 sync ./built-shell "s3://$SITE_BUCKET" --delete
aws cloudfront create-invalidation --distribution-id "$DISTRIBUTION_ID" --paths / /index.html
```

## Keeping private content out of the public bucket

**Everything in the S3 bucket is readable by anyone who can reach the
distribution.** Only the generic renderer shell belongs there — the HTML, JS,
CSS, fonts, and icons that are byte-identical for every viewer. Saga
narratives, review threads, approvals, landmarks, and source diffs are private,
per-repository data and must be served through an authenticated path.

[`lib/site-assets.ts`](lib/site-assets.ts) enforces this at synthesis time.
When `reviewSaga:siteAssetPath` is set, the directory is rejected if it
contains any of:

- `*.saga`, `*.chapter`, `*.fragment`, `*.landmark`, `*.thread`, `*.message`
  directories;
- the reserved overlays `___diffs`, `___review`, `___approvals`, `___landmarks`;
- saga manifests (`saga.json`, `chapter.json`, `section.json`, `fragment.json`,
  `thread.json`, `landmark.json`, `review-event.json`);
- credential-shaped files (`.env*`, `*.pem`, `*.key`, `id_rsa`, `.npmrc`, …);
- `.git`, `.aws`, `.ssh`, or `node_modules`, which indicate a checkout was
  passed by mistake.

It also requires an `index.html` entrypoint. Pointing the context key at this
repository's own `review-saga-v2.saga` directory fails the build with a list of
every offending path — there is a test that asserts exactly that.

## Security boundaries

**What is enforced today**

- The S3 bucket has Block Public Access fully on, no website configuration, and
  no public policy. CloudFront reaches it through **Origin Access Control**;
  the bucket policy allows `s3:GetObject` for the CloudFront service principal
  only when `AWS:SourceArn` is this distribution. The legacy Origin Access
  Identity is not used, and a test asserts none exists.
- Every bucket denies non-TLS requests and requires TLS 1.2.
- CloudFront redirects HTTP to HTTPS for the shell, and requires HTTPS for
  `/api/*`.
- Response headers on the shell: HSTS (1 year, `includeSubDomains`),
  `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer`, a Content-Security-Policy mirroring the local
  reviewer's policy in `internal/server/server.go`, plus `Permissions-Policy`,
  `Cross-Origin-Opener-Policy`, and `Cross-Origin-Resource-Policy`. The
  `Server` header is removed.
- `/api/*` gets the same headers plus `Cache-Control: no-store` and a
  `default-src 'none'` CSP.
- The Lambda runs on ARM64 with a 10-second timeout and its own log group. Its
  role gets basic logging only — plus, if and only if
  `reviewSaga:githubAppSecretArn` is set, `secretsmanager:GetSecretValue`
  scoped to that one ARN. A test fails the build if any `Allow` statement
  pairs a `secretsmanager:`/`s3:`/`iam:`/`sts:` action with `Resource: "*"`.
- No credential is embedded anywhere. Secrets are referenced by ARN and read at
  runtime; `/api/config` reports only booleans.
- The HTTP API stage is throttled (25 rps / 50 burst by default) and writes
  structured JSON access logs.

**What is not enforced yet — known gaps**

- **The HTTP API origin is reachable directly**, at the `HttpApiEndpoint`
  output, bypassing CloudFront. That is acceptable while the only routes are an
  unauthenticated health check and a secret-free config document, but it must be
  closed before any authenticated route is added. Options, in rough order of
  preference: a Lambda authorizer that requires the session cookie CloudFront
  forwards; AWS WAF on the distribution plus a shared secret header injected by
  CloudFront; or a custom domain on the API with
  `disableExecuteApiEndpoint`.
- **`minimumProtocolVersion` only applies with a custom certificate.** The
  default `*.cloudfront.net` certificate is fixed at TLSv1 by CloudFront, so
  the stack sets the TLS 1.2 policy only when a domain and certificate are
  configured. Production deployments should use a custom domain.
- There is no WAF, no rate limiting per identity, and no bot control.
- CloudFront access logs are written to a bucket in the same account; there is
  no cross-account log archive.

## Cost drivers

At idle the stack costs close to nothing — everything except the log and bucket
storage is request-priced. The drivers, roughly in the order they will bite:

| Driver                        | Notes                                                                                                                                                                       |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| CloudFront data transfer out  | The dominant cost for a popular saga. `PriceClass_100` (default) limits edge locations to North America and Europe, which is both cheaper and usually sufficient.           |
| CloudFront requests           | HTTPS request pricing; the immutable `/assets/*` behaviour keeps repeat views off the origin entirely.                                                                      |
| CloudWatch Logs ingestion     | Usually the biggest _surprise_ line item. Both log groups honour `reviewSaga:logRetentionDays` (90 days by default); lower it for chatty environments.                      |
| API Gateway HTTP API requests | Per-million pricing, and roughly a third the cost of a REST API — which is why HTTP API was chosen.                                                                         |
| Lambda                        | 256 MB ARM64, a few milliseconds per request; effectively free at review-tool traffic levels.                                                                               |
| S3 storage and requests       | The shell is a few megabytes. Non-current versions expire after 30 days; CloudFront access logs expire on the retention schedule.                                           |
| Route 53                      | Only if the stack manages DNS: a hosted zone has a flat monthly fee, plus per-query charges.                                                                                |
| CloudFront invalidations      | The first 1,000 paths per month are free; invalidating `/*` on every deploy is the way to exceed that. Invalidate `/` and `/index.html` and rely on content hashes instead. |

Set `reviewSaga:enableAccessLogs=false` and a short
`reviewSaga:logRetentionDays` for throwaway preview environments.

## GitHub App secret placeholders

Nothing is created for the GitHub App yet, and **no secret value belongs in this
repository or in the CloudFormation template**. The intended shape:

```sh
aws secretsmanager create-secret \
  --name review-saga/github-app \
  --description "Review Saga GitHub App credentials" \
  --secret-string '{
    "appId": "REPLACE_ME",
    "clientId": "REPLACE_ME",
    "clientSecret": "REPLACE_ME",
    "privateKey": "-----BEGIN RSA PRIVATE KEY-----\nREPLACE_ME\n-----END RSA PRIVATE KEY-----",
    "webhookSecret": "REPLACE_ME"
  }'
```

Then deploy with the ARN — only the ARN:

```sh
npm run deploy -- \
  -c reviewSaga:githubAppSecretArn=arn:aws:secretsmanager:eu-west-1:111122223333:secret:review-saga/github-app-AbCdEf \
  -c reviewSaga:githubAppId=123456
```

The handler receives `GITHUB_APP_SECRET_ARN` and `GITHUB_APP_ID` as environment
variables and is granted `secretsmanager:GetSecretValue` on that ARN alone. It
reports `features.githubApp: true` from `/api/config` and never returns the
value. Rotating the secret needs no redeploy.

## API surface

`GET /api/health` — liveness and build identity:

```json
{
  "status": "ok",
  "service": "review-saga",
  "environment": "prod",
  "version": "1.4.0",
  "time": "2026-08-20T09:00:00.000Z",
  "requestId": "…"
}
```

`GET /api/config` — public, non-secret runtime configuration. The shell is a
static cacheable artifact, so it cannot be built per environment; it fetches
this at startup instead:

```json
{
  "environment": "prod",
  "version": "1.4.0",
  "siteUrl": "https://saga.example.com",
  "apiBasePath": "/api",
  "sagaSource": "none",
  "features": { "githubApp": false, "commentPosting": false, "privateRepositories": false }
}
```

Unknown paths return a JSON `404`; a wrong method returns `405` with `Allow`.
Both responses are `no-store`.

`siteUrl` is only populated when a custom domain is configured: deriving it from
the distribution would create a CloudFormation dependency cycle
(Lambda → Distribution → HTTP API → Integration → Lambda). Without a domain the
shell should fall back to its own `location.origin`.

## Outputs

`SiteUrl`, `SiteBucketName`, `SiteBucketArn`, `DistributionId`,
`DistributionDomainName`, `ApiHealthUrl`, `HttpApiId`, `HttpApiEndpoint`,
`ApiFunctionName`, `ApiFunctionLogGroupName`, `ApiAccessLogGroupName`, and —
when configured — `AccessLogBucketName` and `CustomDomainName`. Each is
exported as `<appName>-<environment>-<OutputName>`.

```sh
aws cloudformation describe-stacks --stack-name review-saga-dev-hosting \
  --query 'Stacks[0].Outputs' --output table
```

## Tests

```sh
npm test
```

- `test/config.test.ts` — defaults, environment fallback, and every validation rule.
- `test/site-assets.test.ts` — the private-content guard, including a case that
  points it at this repository's real `review-saga-v2.saga` directory.
- `test/hosting-stack.test.ts` — CDK assertions over the synthesized template:
  public access blocks, OAC and the scoped bucket policy, `/api/*` routing and
  cache separation, response headers, throttling and access logs, Lambda
  configuration, IAM least privilege, outputs, custom domain, and removal
  policies.
- `test/api-handler.test.ts` — the health/config handler's behaviour, including
  that a configured secret ARN never appears in a response body.

Tests run serially (`maxWorkers: 1` in `jest.config.js`): each one synthesizes a
full stack, which is I/O bound, and parallel workers contend badly.

## Next steps

**Shared API groundwork**

1. Close the direct-origin gap described under [Security boundaries](#security-boundaries).
2. Add the GitHub App secret and a Lambda authorizer; issue a host-only,
   `HttpOnly`, `SameSite=Strict` session cookie.
3. Add `POST /api/comments` and the GitHub OAuth callback routes to the existing
   HTTP API. The routing, throttling, logging, and IAM shape are already here.

**Public repository rendering** — the simpler case, and the one to build first.
The saga and the code are already world-readable, so a saga can be rendered
without a viewer identity. Fetch the saga from the source repository at request
time (or on a webhook, into a cache), and serve it from a content endpoint on
the same HTTP API. Nothing needs to move into the S3 bucket, so the public
origin keeps holding the shell alone. Watch for GitHub API rate limits — this is
the reason to add a cache, not a reason to pre-publish content into S3.

**Private repository rendering** — requires the authenticated path.
The viewer signs in with the GitHub App, the authorizer checks that their token
grants access to the source repository, and only then does the content endpoint
return the saga. Two consequences for this stack:

- private saga content must never be written to the S3 origin, because OAC
  makes it readable to every viewer of the distribution — the guard in
  `lib/site-assets.ts` exists precisely to keep that mistake out of a deploy;
- `/api/*` is already `no-store` and uncached at the edge, so per-viewer
  responses cannot leak into a shared cache.

A likely later addition is a second, _authenticated_ content store (a separate
private bucket read through a signed-URL or Lambda-mediated path, or DynamoDB
for review overlay data). That belongs in its own stack or construct, not in the
public shell bucket.

**Operational**

- CloudWatch alarms on 5xx rate, Lambda errors, and API latency.
- A CI job running `npm run check`, and `cdk diff` against the deployed stack.
- AWS WAF once there is anything worth attacking.
