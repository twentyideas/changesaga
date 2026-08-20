import * as path from 'path';
import { Match, Template } from 'aws-cdk-lib/assertions';
import * as cloudfront from 'aws-cdk-lib/aws-cloudfront';

import { CONTEXT_KEYS } from '../lib/config';
import { SiteAssetError } from '../lib/site-assets';
import { synth } from './helpers';

const CERTIFICATE_ARN =
  'arn:aws:acm:us-east-1:111122223333:certificate/6b1f0a3c-1c0e-4c9f-9a8a-2f5f0b4c1d2e';
const SECRET_ARN =
  'arn:aws:secretsmanager:eu-west-1:111122223333:secret:review-saga/github-app-AbCdEf';

let template: Template;

beforeAll(() => {
  template = synth().template;
});

describe('static origin', () => {
  test('every bucket blocks public access, requires TLS, and is encrypted', () => {
    const buckets = template.findResources('AWS::S3::Bucket');
    expect(Object.keys(buckets).length).toBeGreaterThanOrEqual(2);
    for (const bucket of Object.values(buckets)) {
      expect(bucket.Properties.PublicAccessBlockConfiguration).toEqual({
        BlockPublicAcls: true,
        BlockPublicPolicy: true,
        IgnorePublicAcls: true,
        RestrictPublicBuckets: true,
      });
      expect(bucket.Properties.BucketEncryption).toBeDefined();
    }

    const policies = template.findResources('AWS::S3::BucketPolicy');
    for (const policy of Object.values(policies)) {
      const statements = policy.Properties.PolicyDocument.Statement as Array<Record<string, any>>;
      const denyInsecure = statements.find(
        (statement) =>
          statement.Effect === 'Deny' &&
          statement.Condition?.Bool?.['aws:SecureTransport'] === 'false',
      );
      expect(denyInsecure).toBeDefined();
    }
  });

  test('the shell bucket is versioned so a bad release can be rolled back', () => {
    template.hasResourceProperties('AWS::S3::Bucket', {
      VersioningConfiguration: { Status: 'Enabled' },
    });
  });

  test('the bucket has no website configuration and is reachable only via CloudFront', () => {
    for (const bucket of Object.values(template.findResources('AWS::S3::Bucket'))) {
      expect(bucket.Properties.WebsiteConfiguration).toBeUndefined();
    }
  });
});

describe('origin access control', () => {
  test('uses OAC rather than the legacy origin access identity', () => {
    template.resourceCountIs('AWS::CloudFront::OriginAccessControl', 1);
    template.resourceCountIs('AWS::CloudFront::CloudFrontOriginAccessIdentity', 0);
    template.hasResourceProperties('AWS::CloudFront::OriginAccessControl', {
      OriginAccessControlConfig: Match.objectLike({
        OriginAccessControlOriginType: 's3',
        SigningBehavior: 'always',
        SigningProtocol: 'sigv4',
      }),
    });
  });

  test('the bucket policy grants read only to this distribution', () => {
    const policies = Object.values(template.findResources('AWS::S3::BucketPolicy'));
    const statements = policies.flatMap(
      (policy) => policy.Properties.PolicyDocument.Statement as Array<Record<string, any>>,
    );
    const cloudFrontGrant = statements.find(
      (statement) => statement.Principal?.Service === 'cloudfront.amazonaws.com',
    );
    expect(cloudFrontGrant).toBeDefined();
    expect(cloudFrontGrant!.Effect).toBe('Allow');
    expect(cloudFrontGrant!.Action).toBe('s3:GetObject');
    // Scoped to the one distribution, so another account's CloudFront cannot read.
    expect(JSON.stringify(cloudFrontGrant!.Condition)).toContain('AWS:SourceArn');
  });
});

describe('distribution', () => {
  test('redirects to HTTPS, enforces a modern TLS policy, and enables logging', () => {
    template.hasResourceProperties('AWS::CloudFront::Distribution', {
      DistributionConfig: Match.objectLike({
        Enabled: true,
        HttpVersion: 'http2and3',
        IPV6Enabled: true,
        DefaultRootObject: 'index.html',
        DefaultCacheBehavior: Match.objectLike({
          ViewerProtocolPolicy: 'redirect-to-https',
          Compress: true,
        }),
        Logging: Match.objectLike({ Prefix: 'cloudfront/' }),
      }),
    });
  });

  test('routes /api/* to the HTTP API origin with caching disabled', () => {
    const distribution = Object.values(template.findResources('AWS::CloudFront::Distribution'))[0]
      .Properties.DistributionConfig;
    const behaviours = distribution.CacheBehaviors as Array<Record<string, any>>;
    const api = behaviours.find((behaviour) => behaviour.PathPattern === '/api/*');

    expect(api).toBeDefined();
    expect(api!.ViewerProtocolPolicy).toBe('https-only');
    expect(api!.CachePolicyId).toBe(cloudfront.CachePolicy.CACHING_DISABLED.cachePolicyId);
    expect(api!.OriginRequestPolicyId).toBe(
      cloudfront.OriginRequestPolicy.ALL_VIEWER_EXCEPT_HOST_HEADER.originRequestPolicyId,
    );
    expect(api!.AllowedMethods).toEqual(
      expect.arrayContaining(['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS']),
    );

    // The API behaviour must not point at the S3 origin.
    const origins = distribution.Origins as Array<Record<string, any>>;
    const apiOrigin = origins.find((origin) => origin.Id === api!.TargetOriginId);
    expect(apiOrigin!.CustomOriginConfig).toBeDefined();
    expect(apiOrigin!.CustomOriginConfig.OriginProtocolPolicy).toBe('https-only');
    expect(apiOrigin!.S3OriginConfig).toBeUndefined();
  });

  test('separates shell, immutable asset, and API caching', () => {
    const distribution = Object.values(template.findResources('AWS::CloudFront::Distribution'))[0]
      .Properties.DistributionConfig;
    const behaviours = distribution.CacheBehaviors as Array<Record<string, any>>;
    const assets = behaviours.find((behaviour) => behaviour.PathPattern === '/assets/*');
    expect(assets).toBeDefined();

    const policies = template.findResources('AWS::CloudFront::CachePolicy');
    const ttls = Object.values(policies).map((policy) => policy.Properties.CachePolicyConfig);
    const shell = ttls.find((config) => String(config.Name).endsWith('-shell'));
    const immutable = ttls.find((config) => String(config.Name).endsWith('-immutable-assets'));

    expect(shell.DefaultTTL).toBe(300);
    expect(shell.MaxTTL).toBe(3600);
    expect(immutable.DefaultTTL).toBe(31536000);
    expect(immutable.MinTTL).toBe(31536000);

    // Shell documents and immutable assets use different policies from each other.
    expect(assets!.CachePolicyId).not.toBe(distribution.DefaultCacheBehavior.CachePolicyId);
  });

  test('rewrites directory URLs on the shell behaviour only', () => {
    template.resourceCountIs('AWS::CloudFront::Function', 1);
    const distribution = Object.values(template.findResources('AWS::CloudFront::Distribution'))[0]
      .Properties.DistributionConfig;
    expect(distribution.DefaultCacheBehavior.FunctionAssociations).toHaveLength(1);
    const api = (distribution.CacheBehaviors as Array<Record<string, any>>).find(
      (behaviour) => behaviour.PathPattern === '/api/*',
    );
    expect(api!.FunctionAssociations ?? []).toHaveLength(0);
  });

  test('does not use custom error responses, which would also rewrite API errors', () => {
    const distribution = Object.values(template.findResources('AWS::CloudFront::Distribution'))[0]
      .Properties.DistributionConfig;
    expect(distribution.CustomErrorResponses).toBeUndefined();
  });

  test('serves the CloudFront certificate when no custom domain is configured', () => {
    const distribution = Object.values(template.findResources('AWS::CloudFront::Distribution'))[0]
      .Properties.DistributionConfig;
    expect(distribution.Aliases).toBeUndefined();
    // CloudFront emits no ViewerCertificate block for the default *.cloudfront.net
    // certificate, which also means the TLSv1.2_2021 policy only takes effect
    // once a custom domain is configured. See README "Security boundaries".
    expect(distribution.ViewerCertificate).toBeUndefined();
  });
});

describe('response headers', () => {
  test('the shell policy sets HSTS, a CSP, and frame denial', () => {
    const policies = Object.values(
      template.findResources('AWS::CloudFront::ResponseHeadersPolicy'),
    );
    const shell = policies.find((policy) =>
      String(policy.Properties.ResponseHeadersPolicyConfig.Name).endsWith('-shell-headers'),
    );
    expect(shell).toBeDefined();

    const security = shell!.Properties.ResponseHeadersPolicyConfig.SecurityHeadersConfig;
    expect(security.ContentTypeOptions).toEqual({ Override: true });
    expect(security.FrameOptions).toEqual({ FrameOption: 'DENY', Override: true });
    expect(security.ReferrerPolicy.ReferrerPolicy).toBe('no-referrer');
    expect(security.StrictTransportSecurity.AccessControlMaxAgeSec).toBe(31536000);
    expect(security.StrictTransportSecurity.IncludeSubdomains).toBe(true);
    expect(security.ContentSecurityPolicy.ContentSecurityPolicy).toContain("default-src 'self'");
    expect(security.ContentSecurityPolicy.ContentSecurityPolicy).toContain(
      "frame-ancestors 'none'",
    );
    expect(security.ContentSecurityPolicy.ContentSecurityPolicy).toContain("object-src 'none'");
  });

  test('the API policy forbids caching and framing', () => {
    const policies = Object.values(
      template.findResources('AWS::CloudFront::ResponseHeadersPolicy'),
    );
    const api = policies.find((policy) =>
      String(policy.Properties.ResponseHeadersPolicyConfig.Name).endsWith('-api-headers'),
    );
    expect(api).toBeDefined();

    const custom = api!.Properties.ResponseHeadersPolicyConfig.CustomHeadersConfig.Items as Array<
      Record<string, any>
    >;
    const cacheControl = custom.find((item) => item.Header === 'Cache-Control');
    expect(cacheControl).toEqual({ Header: 'Cache-Control', Value: 'no-store', Override: true });
    expect(
      api!.Properties.ResponseHeadersPolicyConfig.SecurityHeadersConfig.ContentSecurityPolicy
        .ContentSecurityPolicy,
    ).toContain("default-src 'none'");
  });
});

describe('API boundary', () => {
  test('creates one HTTP API with explicit health and config routes', () => {
    template.resourceCountIs('AWS::ApiGatewayV2::Api', 1);
    template.hasResourceProperties('AWS::ApiGatewayV2::Api', { ProtocolType: 'HTTP' });

    const routes = Object.values(template.findResources('AWS::ApiGatewayV2::Route')).map(
      (route) => route.Properties.RouteKey,
    );
    expect(routes.sort()).toEqual(['GET /api/config', 'GET /api/health']);
  });

  test('the stage throttles by default and writes access logs', () => {
    template.hasResourceProperties('AWS::ApiGatewayV2::Stage', {
      StageName: '$default',
      AutoDeploy: true,
      DefaultRouteSettings: Match.objectLike({ ThrottlingRateLimit: 25, ThrottlingBurstLimit: 50 }),
      AccessLogSettings: Match.objectLike({ DestinationArn: Match.anyValue() }),
    });
  });

  test('the handler is small, current, and time limited', () => {
    template.hasResourceProperties('AWS::Lambda::Function', {
      Runtime: 'nodejs22.x',
      Architectures: ['arm64'],
      MemorySize: 256,
      Timeout: 10,
      Handler: 'index.handler',
    });
  });

  test('log groups have a retention policy', () => {
    const groups = Object.values(template.findResources('AWS::Logs::LogGroup'));
    expect(groups.length).toBeGreaterThanOrEqual(2);
    for (const group of groups) {
      expect(group.Properties.RetentionInDays).toBe(90);
    }
  });

  test('the handler environment carries no secret material', () => {
    const functions = Object.values(template.findResources('AWS::Lambda::Function'));
    const handler = functions.find(
      (fn) => fn.Properties.Environment?.Variables?.APP_NAME !== undefined,
    );
    const variables = handler!.Properties.Environment.Variables as Record<string, string>;
    expect(Object.keys(variables).sort()).toEqual(['APP_ENVIRONMENT', 'APP_NAME', 'APP_VERSION']);
    expect(JSON.stringify(variables)).not.toMatch(/secret|token|password|private[_-]?key/i);
  });
});

describe('least privilege', () => {
  test('no IAM policy grants secretsmanager or s3 on every resource', () => {
    const policies = Object.values(template.findResources('AWS::IAM::Policy'));
    for (const policy of policies) {
      const statements = policy.Properties.PolicyDocument.Statement as Array<Record<string, any>>;
      for (const statement of statements) {
        if (statement.Effect !== 'Allow') {
          continue;
        }
        const actions = ([] as string[]).concat(statement.Action ?? []);
        const wildcardResource = ([] as unknown[]).concat(statement.Resource ?? []).includes('*');
        if (!wildcardResource) {
          continue;
        }
        for (const action of actions) {
          expect(action).not.toMatch(/^(secretsmanager|s3|iam|sts):/);
        }
      }
    }
  });

  test('the handler gets no secret access unless a secret ARN is configured', () => {
    const policies = JSON.stringify(template.findResources('AWS::IAM::Policy'));
    expect(policies).not.toContain('secretsmanager:GetSecretValue');
  });

  test('a configured secret ARN produces a grant scoped to that ARN alone', () => {
    const scoped = synth({ [CONTEXT_KEYS.githubAppSecretArn]: SECRET_ARN }).template;
    const statements = Object.values(scoped.findResources('AWS::IAM::Policy')).flatMap(
      (policy) => policy.Properties.PolicyDocument.Statement as Array<Record<string, any>>,
    );
    const secretStatement = statements.find((statement) =>
      ([] as string[]).concat(statement.Action ?? []).includes('secretsmanager:GetSecretValue'),
    );
    expect(secretStatement).toBeDefined();
    expect(secretStatement!.Resource).toBe(SECRET_ARN);
  });

  test('a configured secret exposes only its ARN to the handler', () => {
    const scoped = synth({
      [CONTEXT_KEYS.githubAppSecretArn]: SECRET_ARN,
      [CONTEXT_KEYS.githubAppId]: '123456',
    }).template;
    scoped.hasResourceProperties('AWS::Lambda::Function', {
      Environment: {
        Variables: Match.objectLike({ GITHUB_APP_SECRET_ARN: SECRET_ARN, GITHUB_APP_ID: '123456' }),
      },
    });
  });
});

describe('outputs', () => {
  test('exports the values an operator or CI job needs', () => {
    const outputs = template.toJSON().Outputs as Record<string, { Description: string }>;
    expect(Object.keys(outputs).sort()).toEqual(
      [
        'AccessLogBucketName',
        'ApiAccessLogGroupName',
        'ApiFunctionLogGroupName',
        'ApiFunctionName',
        'ApiHealthUrl',
        'DistributionDomainName',
        'DistributionId',
        'HttpApiEndpoint',
        'HttpApiId',
        'SiteBucketArn',
        'SiteBucketName',
        'SiteUrl',
      ].sort(),
    );
    for (const output of Object.values(outputs)) {
      expect(output.Description).toBeTruthy();
    }
  });
});

describe('custom domain', () => {
  test('adds aliases and the supplied certificate without touching DNS', () => {
    const { template: domainTemplate } = synth({
      [CONTEXT_KEYS.domainName]: 'saga.example.com',
      [CONTEXT_KEYS.alternativeDomainNames]: 'www.saga.example.com',
      [CONTEXT_KEYS.certificateArn]: CERTIFICATE_ARN,
    });
    domainTemplate.hasResourceProperties('AWS::CloudFront::Distribution', {
      DistributionConfig: Match.objectLike({
        Aliases: ['saga.example.com', 'www.saga.example.com'],
        ViewerCertificate: Match.objectLike({
          AcmCertificateArn: CERTIFICATE_ARN,
          MinimumProtocolVersion: 'TLSv1.2_2021',
          SslSupportMethod: 'sni-only',
        }),
      }),
    });
    domainTemplate.resourceCountIs('AWS::Route53::RecordSet', 0);
    domainTemplate.hasOutput('CustomDomainName', { Value: 'saga.example.com' });
  });

  test('creates alias records when a hosted zone is supplied', () => {
    const { template: dnsTemplate } = synth({
      [CONTEXT_KEYS.domainName]: 'saga.example.com',
      [CONTEXT_KEYS.certificateArn]: CERTIFICATE_ARN,
      [CONTEXT_KEYS.hostedZoneId]: 'Z0123456789ABCDEFGHIJ',
      [CONTEXT_KEYS.hostedZoneName]: 'example.com',
    });
    // One A record and one AAAA record for the single alias.
    dnsTemplate.resourceCountIs('AWS::Route53::RecordSet', 2);
    dnsTemplate.hasResourceProperties('AWS::Route53::RecordSet', {
      Type: 'A',
      Name: 'saga.example.com.',
    });
  });
});

describe('shell deployment guard', () => {
  test('deploys a valid renderer shell and invalidates the entrypoint', () => {
    const { template: deployTemplate } = synth({
      [CONTEXT_KEYS.siteAssetPath]: path.join('test', 'fixtures', 'renderer-shell'),
    });
    deployTemplate.resourceCountIs('Custom::CDKBucketDeployment', 1);
    deployTemplate.hasResourceProperties('Custom::CDKBucketDeployment', {
      Prune: true,
      DistributionPaths: ['/', '/index.html'],
    });
  });

  test('refuses to synthesise when saga content is staged into the shell', () => {
    expect(() =>
      synth({ [CONTEXT_KEYS.siteAssetPath]: path.join('..', 'review-saga-v2.saga') }),
    ).toThrow(SiteAssetError);
  });

  test('creates no deployment when no shell path is configured', () => {
    template.resourceCountIs('Custom::CDKBucketDeployment', 0);
  });
});

describe('data protection', () => {
  test('non-production buckets are removable, production buckets are retained', () => {
    const dev = Object.values(template.findResources('AWS::S3::Bucket'));
    for (const bucket of dev) {
      expect(bucket.DeletionPolicy).toBe('Delete');
    }

    const prod = synth({ [CONTEXT_KEYS.environment]: 'prod' }).template;
    for (const bucket of Object.values(prod.findResources('AWS::S3::Bucket'))) {
      expect(bucket.DeletionPolicy).toBe('Retain');
    }
  });

  test('access logging can be turned off for throwaway environments', () => {
    const noLogs = synth({ [CONTEXT_KEYS.enableAccessLogs]: 'false' }).template;
    const distribution = Object.values(noLogs.findResources('AWS::CloudFront::Distribution'))[0]
      .Properties.DistributionConfig;
    expect(distribution.Logging).toBeUndefined();
    expect(Object.keys(noLogs.findResources('AWS::S3::Bucket'))).toHaveLength(1);
  });
});
