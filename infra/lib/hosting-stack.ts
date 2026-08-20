import * as path from 'path';
import * as cdk from 'aws-cdk-lib';
import { Construct } from 'constructs';
import * as acm from 'aws-cdk-lib/aws-certificatemanager';
import * as apigwv2 from 'aws-cdk-lib/aws-apigatewayv2';
import * as integrations from 'aws-cdk-lib/aws-apigatewayv2-integrations';
import * as cloudfront from 'aws-cdk-lib/aws-cloudfront';
import * as origins from 'aws-cdk-lib/aws-cloudfront-origins';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import * as nodejs from 'aws-cdk-lib/aws-lambda-nodejs';
import * as logs from 'aws-cdk-lib/aws-logs';
import * as route53 from 'aws-cdk-lib/aws-route53';
import * as targets from 'aws-cdk-lib/aws-route53-targets';
import * as s3 from 'aws-cdk-lib/aws-s3';
import * as s3deploy from 'aws-cdk-lib/aws-s3-deployment';
import * as secretsmanager from 'aws-cdk-lib/aws-secretsmanager';

import { HostingConfig, PriceClassName, resourcePrefix } from './config';
import { assertRendererShellOnly } from './site-assets';

export interface HostingStackProps extends cdk.StackProps {
  readonly config: HostingConfig;
}

const PRICE_CLASSES: Record<PriceClassName, cloudfront.PriceClass> = {
  PriceClass_100: cloudfront.PriceClass.PRICE_CLASS_100,
  PriceClass_200: cloudfront.PriceClass.PRICE_CLASS_200,
  PriceClass_All: cloudfront.PriceClass.PRICE_CLASS_ALL,
};

/**
 * Hosting foundation for shareable Review Saga sites.
 *
 * Two origins sit behind one CloudFront distribution:
 *
 *   * a private S3 bucket reachable only through Origin Access Control, holding
 *     the *generic renderer shell* (HTML/JS/CSS that is identical for everyone);
 *   * an HTTP API fronting a small Lambda, which is where authenticated work —
 *     GitHub App login, comment posting, private saga content — will be added.
 *
 * Saga narratives, review threads, and source diffs are private per-repository
 * data and are never staged into the public bucket. See `site-assets.ts` for
 * the synth-time guard that enforces this.
 */
export class HostingStack extends cdk.Stack {
  public readonly siteBucket: s3.Bucket;
  public readonly distribution: cloudfront.Distribution;
  public readonly httpApi: apigwv2.HttpApi;
  public readonly apiHandler: nodejs.NodejsFunction;
  public readonly accessLogBucket?: s3.Bucket;

  constructor(scope: Construct, id: string, props: HostingStackProps) {
    super(scope, id, props);

    const config = props.config;
    const prefix = resourcePrefix(config);
    const removalPolicy = config.retainData ? cdk.RemovalPolicy.RETAIN : cdk.RemovalPolicy.DESTROY;
    const autoDeleteObjects = !config.retainData;
    const retention = config.logRetentionDays as logs.RetentionDays;

    cdk.Tags.of(this).add('Application', config.appName);
    cdk.Tags.of(this).add('Environment', config.environmentName);

    // ---------------------------------------------------------------- logging

    if (config.enableAccessLogs) {
      this.accessLogBucket = new s3.Bucket(this, 'AccessLogBucket', {
        blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
        encryption: s3.BucketEncryption.S3_MANAGED,
        enforceSSL: true,
        minimumTLSVersion: 1.2,
        // CloudFront standard logging delivers objects with an ACL, so the
        // bucket cannot use the fully ACL-disabled ownership setting.
        objectOwnership: s3.ObjectOwnership.BUCKET_OWNER_PREFERRED,
        lifecycleRules: [
          {
            id: 'expire-access-logs',
            enabled: true,
            expiration: cdk.Duration.days(config.logRetentionDays),
            abortIncompleteMultipartUploadAfter: cdk.Duration.days(7),
          },
        ],
        removalPolicy,
        autoDeleteObjects,
      });
    }

    // ------------------------------------------------------- static shell origin

    this.siteBucket = new s3.Bucket(this, 'SiteBucket', {
      blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
      publicReadAccess: false,
      encryption: s3.BucketEncryption.S3_MANAGED,
      enforceSSL: true,
      minimumTLSVersion: 1.2,
      // Versioning lets a bad shell release be rolled back without a rebuild.
      versioned: true,
      lifecycleRules: [
        {
          id: 'expire-noncurrent-shell-versions',
          enabled: true,
          noncurrentVersionExpiration: cdk.Duration.days(30),
          abortIncompleteMultipartUploadAfter: cdk.Duration.days(7),
        },
      ],
      removalPolicy,
      autoDeleteObjects,
    });

    // ------------------------------------------------------------- API boundary

    const apiLogGroup = new logs.LogGroup(this, 'ApiHandlerLogGroup', {
      logGroupName: `/aws/lambda/${prefix}-api`,
      retention,
      removalPolicy: cdk.RemovalPolicy.DESTROY,
    });

    const apiAccessLogGroup = new logs.LogGroup(this, 'ApiAccessLogGroup', {
      logGroupName: `/aws/apigateway/${prefix}-api`,
      retention,
      removalPolicy: cdk.RemovalPolicy.DESTROY,
    });

    this.apiHandler = new nodejs.NodejsFunction(this, 'ApiHandler', {
      functionName: `${prefix}-api`,
      description: 'Review Saga health and public runtime configuration',
      entry: path.join(__dirname, '..', 'lambda', 'health', 'index.ts'),
      handler: 'handler',
      runtime: lambda.Runtime.NODEJS_22_X,
      architecture: lambda.Architecture.ARM_64,
      memorySize: 256,
      timeout: cdk.Duration.seconds(10),
      logGroup: apiLogGroup,
      environment: {
        APP_NAME: config.appName,
        APP_ENVIRONMENT: config.environmentName,
        APP_VERSION: config.appVersion,
        // Only set when a custom domain is configured: deriving it from the
        // distribution would create a CloudFormation dependency cycle
        // (Lambda -> Distribution -> HttpApi -> Integration -> Lambda).
        ...(config.domain !== undefined ? { SITE_URL: `https://${config.domain.domainName}` } : {}),
        // Placeholders for the GitHub App work. The ARN is not a credential;
        // the value is read at runtime through the least-privilege grant below.
        ...(config.githubAppSecretArn !== undefined
          ? { GITHUB_APP_SECRET_ARN: config.githubAppSecretArn }
          : {}),
        ...(config.githubAppId !== undefined ? { GITHUB_APP_ID: config.githubAppId } : {}),
      },
      bundling: {
        minify: true,
        sourceMap: false,
        target: 'node22',
        // The handler has no runtime dependencies; nothing is pulled from npm.
        externalModules: [],
      },
    });

    if (config.githubAppSecretArn !== undefined) {
      // Scoped to exactly one secret ARN. No wildcard resources, no inline value.
      const secret = /-[A-Za-z0-9]{6}$/.test(config.githubAppSecretArn)
        ? secretsmanager.Secret.fromSecretCompleteArn(
            this,
            'GitHubAppSecret',
            config.githubAppSecretArn,
          )
        : secretsmanager.Secret.fromSecretPartialArn(
            this,
            'GitHubAppSecret',
            config.githubAppSecretArn,
          );
      secret.grantRead(this.apiHandler);
    }

    this.httpApi = new apigwv2.HttpApi(this, 'HttpApi', {
      apiName: `${prefix}-api`,
      description: 'Review Saga API boundary (health/config today; GitHub App endpoints later)',
      // The stage is created explicitly so throttling and access logs are set.
      createDefaultStage: false,
      // No CORS configuration: the shell and the API share one CloudFront origin.
    });

    const apiIntegration = new integrations.HttpLambdaIntegration(
      'ApiHandlerIntegration',
      this.apiHandler,
    );

    this.httpApi.addRoutes({
      path: '/api/health',
      methods: [apigwv2.HttpMethod.GET],
      integration: apiIntegration,
    });
    this.httpApi.addRoutes({
      path: '/api/config',
      methods: [apigwv2.HttpMethod.GET],
      integration: apiIntegration,
    });

    const apiStage = new apigwv2.HttpStage(this, 'ApiDefaultStage', {
      httpApi: this.httpApi,
      autoDeploy: true,
      throttle: {
        rateLimit: config.apiThrottle.rateLimit,
        burstLimit: config.apiThrottle.burstLimit,
      },
    });

    const cfnStage = apiStage.node.defaultChild as apigwv2.CfnStage;
    cfnStage.accessLogSettings = {
      destinationArn: apiAccessLogGroup.logGroupArn,
      format: JSON.stringify({
        requestId: '$context.requestId',
        ip: '$context.identity.sourceIp',
        requestTime: '$context.requestTime',
        httpMethod: '$context.httpMethod',
        routeKey: '$context.routeKey',
        status: '$context.status',
        protocol: '$context.protocol',
        responseLength: '$context.responseLength',
        integrationError: '$context.integrationErrorMessage',
      }),
    };

    // --------------------------------------------------- edge behaviour policies

    const securityHeaders: cloudfront.ResponseCustomHeader[] = [
      {
        header: 'Permissions-Policy',
        value:
          'accelerometer=(), camera=(), geolocation=(), gyroscope=(), microphone=(), payment=(), usb=()',
        override: true,
      },
      { header: 'Cross-Origin-Opener-Policy', value: 'same-origin', override: true },
      { header: 'Cross-Origin-Resource-Policy', value: 'same-origin', override: true },
    ];

    const strictTransportSecurity: cloudfront.ResponseHeadersStrictTransportSecurity = {
      accessControlMaxAge: cdk.Duration.days(365),
      includeSubdomains: true,
      // `preload` is deliberately off: it is an irreversible commitment for the
      // whole apex domain and must be an explicit operator decision.
      preload: false,
      override: true,
    };

    const shellResponseHeaders = new cloudfront.ResponseHeadersPolicy(
      this,
      'ShellResponseHeaders',
      {
        responseHeadersPolicyName: `${prefix}-shell-headers`,
        comment: 'Security headers for the Review Saga renderer shell',
        securityHeadersBehavior: {
          contentTypeOptions: { override: true },
          frameOptions: { frameOption: cloudfront.HeadersFrameOption.DENY, override: true },
          referrerPolicy: {
            referrerPolicy: cloudfront.HeadersReferrerPolicy.NO_REFERRER,
            override: true,
          },
          strictTransportSecurity,
          contentSecurityPolicy: {
            // Mirrors the local reviewer's policy in internal/server/server.go and
            // additionally allows same-origin XHR to /api/*.
            contentSecurityPolicy: [
              "default-src 'self'",
              "script-src 'self'",
              "style-src 'self' 'unsafe-inline'",
              "img-src 'self' data: blob:",
              "font-src 'self'",
              "connect-src 'self'",
              "form-action 'self'",
              "base-uri 'self'",
              "frame-ancestors 'none'",
              "object-src 'none'",
            ].join('; '),
            override: true,
          },
        },
        customHeadersBehavior: { customHeaders: securityHeaders },
        removeHeaders: ['server'],
      },
    );

    const apiResponseHeaders = new cloudfront.ResponseHeadersPolicy(this, 'ApiResponseHeaders', {
      responseHeadersPolicyName: `${prefix}-api-headers`,
      comment: 'Security headers for the Review Saga API boundary',
      securityHeadersBehavior: {
        contentTypeOptions: { override: true },
        frameOptions: { frameOption: cloudfront.HeadersFrameOption.DENY, override: true },
        referrerPolicy: {
          referrerPolicy: cloudfront.HeadersReferrerPolicy.NO_REFERRER,
          override: true,
        },
        strictTransportSecurity,
        contentSecurityPolicy: {
          contentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
          override: true,
        },
      },
      customHeadersBehavior: {
        customHeaders: [
          ...securityHeaders,
          // API responses are per-viewer by construction once auth exists.
          { header: 'Cache-Control', value: 'no-store', override: true },
        ],
      },
      removeHeaders: ['server'],
    });

    const shellCachePolicy = new cloudfront.CachePolicy(this, 'ShellCachePolicy', {
      cachePolicyName: `${prefix}-shell`,
      comment: 'Short TTL for renderer shell documents so releases roll out quickly',
      minTtl: cdk.Duration.seconds(0),
      defaultTtl: cdk.Duration.minutes(5),
      maxTtl: cdk.Duration.hours(1),
      cookieBehavior: cloudfront.CacheCookieBehavior.none(),
      headerBehavior: cloudfront.CacheHeaderBehavior.none(),
      queryStringBehavior: cloudfront.CacheQueryStringBehavior.none(),
      enableAcceptEncodingBrotli: true,
      enableAcceptEncodingGzip: true,
    });

    const immutableCachePolicy = new cloudfront.CachePolicy(this, 'ImmutableAssetCachePolicy', {
      cachePolicyName: `${prefix}-immutable-assets`,
      comment: 'Long TTL for content-hashed shell assets under /assets/*',
      minTtl: cdk.Duration.days(365),
      defaultTtl: cdk.Duration.days(365),
      maxTtl: cdk.Duration.days(365),
      cookieBehavior: cloudfront.CacheCookieBehavior.none(),
      headerBehavior: cloudfront.CacheHeaderBehavior.none(),
      queryStringBehavior: cloudfront.CacheQueryStringBehavior.none(),
      enableAcceptEncodingBrotli: true,
      enableAcceptEncodingGzip: true,
    });

    // Directory-style URLs resolve to index.html. This is attached only to the
    // shell behaviour; CloudFront custom error responses are deliberately not
    // used because they would also rewrite genuine 404s coming from /api/*.
    const directoryIndex = new cloudfront.Function(this, 'DirectoryIndexFunction', {
      functionName: `${prefix}-directory-index`,
      runtime: cloudfront.FunctionRuntime.JS_2_0,
      comment: 'Rewrites directory-style URLs to the shell index document',
      code: cloudfront.FunctionCode.fromInline(
        [
          'function handler(event) {',
          '  var request = event.request;',
          '  var uri = request.uri;',
          "  if (uri.charAt(uri.length - 1) === '/') {",
          "    request.uri = uri + 'index.html';",
          '    return request;',
          '  }',
          "  var lastSegment = uri.substring(uri.lastIndexOf('/') + 1);",
          "  if (lastSegment.indexOf('.') === -1) {",
          "    request.uri = uri + '/index.html';",
          '  }',
          '  return request;',
          '}',
        ].join('\n'),
      ),
    });

    // ------------------------------------------------------------- distribution

    const shellOrigin = origins.S3BucketOrigin.withOriginAccessControl(this.siteBucket, {
      originAccessLevels: [cloudfront.AccessLevel.READ],
    });

    const apiOrigin = new origins.HttpOrigin(cdk.Fn.parseDomainName(this.httpApi.apiEndpoint), {
      protocolPolicy: cloudfront.OriginProtocolPolicy.HTTPS_ONLY,
      originSslProtocols: [cloudfront.OriginSslPolicy.TLS_V1_2],
      readTimeout: cdk.Duration.seconds(30),
      keepaliveTimeout: cdk.Duration.seconds(5),
    });

    const shellBehaviour: cloudfront.BehaviorOptions = {
      origin: shellOrigin,
      viewerProtocolPolicy: cloudfront.ViewerProtocolPolicy.REDIRECT_TO_HTTPS,
      allowedMethods: cloudfront.AllowedMethods.ALLOW_GET_HEAD_OPTIONS,
      cachedMethods: cloudfront.CachedMethods.CACHE_GET_HEAD_OPTIONS,
      compress: true,
      cachePolicy: shellCachePolicy,
      responseHeadersPolicy: shellResponseHeaders,
      functionAssociations: [
        { function: directoryIndex, eventType: cloudfront.FunctionEventType.VIEWER_REQUEST },
      ],
    };

    this.distribution = new cloudfront.Distribution(this, 'Distribution', {
      comment: `${prefix} Review Saga hosting`,
      defaultRootObject: 'index.html',
      httpVersion: cloudfront.HttpVersion.HTTP2_AND_3,
      priceClass: PRICE_CLASSES[config.priceClass],
      enableIpv6: true,
      enableLogging: this.accessLogBucket !== undefined,
      ...(this.accessLogBucket !== undefined
        ? {
            logBucket: this.accessLogBucket,
            logFilePrefix: 'cloudfront/',
            logIncludesCookies: false,
          }
        : {}),
      // The minimum TLS policy is only meaningful alongside a custom certificate:
      // the default *.cloudfront.net certificate is fixed at TLSv1 by CloudFront.
      ...(config.domain !== undefined
        ? {
            domainNames: [config.domain.domainName, ...config.domain.alternativeNames],
            certificate: acm.Certificate.fromCertificateArn(
              this,
              'SiteCertificate',
              config.domain.certificateArn,
            ),
            minimumProtocolVersion: cloudfront.SecurityPolicyProtocol.TLS_V1_2_2021,
          }
        : {}),
      defaultBehavior: shellBehaviour,
      additionalBehaviors: {
        // Content-hashed build output: cache hard, never revalidate.
        '/assets/*': {
          ...shellBehaviour,
          cachePolicy: immutableCachePolicy,
          functionAssociations: [],
        },
        // The API boundary: never cached, full request forwarded except Host.
        '/api/*': {
          origin: apiOrigin,
          viewerProtocolPolicy: cloudfront.ViewerProtocolPolicy.HTTPS_ONLY,
          allowedMethods: cloudfront.AllowedMethods.ALLOW_ALL,
          cachedMethods: cloudfront.CachedMethods.CACHE_GET_HEAD,
          compress: true,
          cachePolicy: cloudfront.CachePolicy.CACHING_DISABLED,
          originRequestPolicy: cloudfront.OriginRequestPolicy.ALL_VIEWER_EXCEPT_HOST_HEADER,
          responseHeadersPolicy: apiResponseHeaders,
        },
      },
    });

    // CloudFront must not be created before the stage that serves /api/*.
    this.distribution.node.addDependency(apiStage);

    // ------------------------------------------------------------- DNS (optional)

    if (config.domain?.hostedZoneId !== undefined && config.domain.hostedZoneName !== undefined) {
      const zone = route53.HostedZone.fromHostedZoneAttributes(this, 'HostedZone', {
        hostedZoneId: config.domain.hostedZoneId,
        zoneName: config.domain.hostedZoneName,
      });
      const aliasTarget = route53.RecordTarget.fromAlias(
        new targets.CloudFrontTarget(this.distribution),
      );
      const names = [config.domain.domainName, ...config.domain.alternativeNames];
      names.forEach((recordName, index) => {
        new route53.ARecord(this, `AliasRecord${index}`, {
          zone,
          recordName,
          target: aliasTarget,
        });
        new route53.AaaaRecord(this, `AliasRecordIpv6${index}`, {
          zone,
          recordName,
          target: aliasTarget,
        });
      });
    }

    // ------------------------------------------------- optional shell deployment

    if (config.siteAssetPath !== undefined) {
      // Fails synthesis if anything private was staged into the shell directory.
      assertRendererShellOnly(config.siteAssetPath);
      new s3deploy.BucketDeployment(this, 'ShellDeployment', {
        sources: [s3deploy.Source.asset(config.siteAssetPath)],
        destinationBucket: this.siteBucket,
        distribution: this.distribution,
        distributionPaths: ['/', '/index.html'],
        prune: true,
        memoryLimit: 256,
      });
    }

    // ----------------------------------------------------------------- outputs

    const output = (id: string, value: string, description: string): void => {
      new cdk.CfnOutput(this, id, { value, description, exportName: `${prefix}-${id}` });
    };

    output(
      'SiteBucketName',
      this.siteBucket.bucketName,
      'Private S3 bucket holding the renderer shell',
    );
    output('SiteBucketArn', this.siteBucket.bucketArn, 'ARN of the renderer shell bucket');
    output(
      'DistributionId',
      this.distribution.distributionId,
      'CloudFront distribution id (use for invalidations)',
    );
    output(
      'DistributionDomainName',
      this.distribution.distributionDomainName,
      'CloudFront domain name',
    );
    output(
      'SiteUrl',
      config.domain !== undefined
        ? `https://${config.domain.domainName}`
        : `https://${this.distribution.distributionDomainName}`,
      'Public entrypoint for the Review Saga site',
    );
    output(
      'ApiHealthUrl',
      `${
        config.domain !== undefined
          ? `https://${config.domain.domainName}`
          : `https://${this.distribution.distributionDomainName}`
      }/api/health`,
      'Health endpoint through CloudFront',
    );
    output('HttpApiId', this.httpApi.apiId, 'HTTP API id');
    output(
      'HttpApiEndpoint',
      this.httpApi.apiEndpoint,
      'Direct HTTP API endpoint (origin; prefer the CloudFront URL)',
    );
    output('ApiFunctionName', this.apiHandler.functionName, 'Lambda function serving /api/*');
    output(
      'ApiFunctionLogGroupName',
      apiLogGroup.logGroupName,
      'CloudWatch log group for the API Lambda',
    );
    output(
      'ApiAccessLogGroupName',
      apiAccessLogGroup.logGroupName,
      'CloudWatch log group for HTTP API access logs',
    );
    if (this.accessLogBucket !== undefined) {
      output(
        'AccessLogBucketName',
        this.accessLogBucket.bucketName,
        'Bucket receiving CloudFront access logs',
      );
    }
    if (config.domain !== undefined) {
      output('CustomDomainName', config.domain.domainName, 'Configured custom domain');
    }
  }
}
