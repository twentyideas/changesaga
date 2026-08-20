import * as path from 'path';
import * as cdk from 'aws-cdk-lib';

import { CONTEXT_KEYS, ConfigError, resolveConfig, resourcePrefix } from '../lib/config';

const PROJECT_ROOT = path.resolve(__dirname, '..');

function resolve(context: Record<string, string>, env: NodeJS.ProcessEnv = {}) {
  const app = new cdk.App({ context });
  return resolveConfig(app.node, { projectRoot: PROJECT_ROOT, env });
}

describe('resolveConfig defaults', () => {
  test('produces a usable configuration with no context at all', () => {
    const config = resolve({});
    expect(config.appName).toBe('review-saga');
    expect(config.environmentName).toBe('dev');
    expect(config.domain).toBeUndefined();
    expect(config.siteAssetPath).toBeUndefined();
    expect(config.githubAppSecretArn).toBeUndefined();
    expect(config.logRetentionDays).toBe(90);
    expect(config.enableAccessLogs).toBe(true);
    expect(config.priceClass).toBe('PriceClass_100');
    expect(config.apiThrottle).toEqual({ rateLimit: 25, burstLimit: 50 });
    expect(resourcePrefix(config)).toBe('review-saga-dev');
  });

  test('retains data by default only in the prod environment', () => {
    expect(resolve({}).retainData).toBe(false);
    expect(resolve({ [CONTEXT_KEYS.environment]: 'prod' }).retainData).toBe(true);
    expect(
      resolve({ [CONTEXT_KEYS.environment]: 'prod', [CONTEXT_KEYS.retainData]: 'false' })
        .retainData,
    ).toBe(false);
  });

  test('falls back to CDK environment variables for the deployment target', () => {
    const config = resolve(
      {},
      { CDK_DEFAULT_ACCOUNT: '111122223333', CDK_DEFAULT_REGION: 'us-east-2' },
    );
    expect(config.account).toBe('111122223333');
    expect(config.region).toBe('us-east-2');
  });

  test('context wins over environment variables', () => {
    const config = resolve(
      { [CONTEXT_KEYS.region]: 'eu-west-1' },
      { CDK_DEFAULT_REGION: 'us-east-2' },
    );
    expect(config.region).toBe('eu-west-1');
  });
});

describe('resolveConfig validation', () => {
  test.each([
    [{ [CONTEXT_KEYS.appName]: 'Review_Saga' }, /appName/],
    [{ [CONTEXT_KEYS.environment]: '9lives' }, /environment/],
    [{ [CONTEXT_KEYS.account]: '123' }, /account/],
    [{ [CONTEXT_KEYS.region]: 'moon-1' }, /region/],
    [{ [CONTEXT_KEYS.logRetentionDays]: '42' }, /logRetentionDays/],
    [{ [CONTEXT_KEYS.logRetentionDays]: 'lots' }, /logRetentionDays/],
    [{ [CONTEXT_KEYS.enableAccessLogs]: 'yes' }, /enableAccessLogs/],
    [{ [CONTEXT_KEYS.priceClass]: 'PriceClass_50' }, /priceClass/],
    [{ [CONTEXT_KEYS.apiRateLimit]: '100', [CONTEXT_KEYS.apiBurstLimit]: '10' }, /apiBurstLimit/],
    [{ [CONTEXT_KEYS.githubAppId]: 'not-a-number' }, /githubAppId/],
    [{ [CONTEXT_KEYS.githubAppSecretArn]: 'my-secret' }, /githubAppSecretArn/],
    [{ [CONTEXT_KEYS.siteAssetPath]: './does-not-exist' }, /siteAssetPath/],
  ])('rejects %p', (context, expected) => {
    expect(() => resolve(context as Record<string, string>)).toThrow(ConfigError);
    expect(() => resolve(context as Record<string, string>)).toThrow(expected);
  });

  test('rejects a site asset path that is a file rather than a directory', () => {
    expect(() => resolve({ [CONTEXT_KEYS.siteAssetPath]: './package.json' })).toThrow(
      /must be a directory/,
    );
  });
});

describe('custom domain configuration', () => {
  const certificateArn =
    'arn:aws:acm:us-east-1:111122223333:certificate/6b1f0a3c-1c0e-4c9f-9a8a-2f5f0b4c1d2e';

  test('accepts a domain with a us-east-1 certificate', () => {
    const config = resolve({
      [CONTEXT_KEYS.domainName]: 'saga.example.com',
      [CONTEXT_KEYS.certificateArn]: certificateArn,
    });
    expect(config.domain?.domainName).toBe('saga.example.com');
    expect(config.domain?.alternativeNames).toEqual([]);
    expect(config.domain?.hostedZoneId).toBeUndefined();
  });

  test('parses a comma-separated alternative name list', () => {
    const config = resolve({
      [CONTEXT_KEYS.domainName]: 'saga.example.com',
      [CONTEXT_KEYS.alternativeDomainNames]: 'www.saga.example.com, review.example.com',
      [CONTEXT_KEYS.certificateArn]: certificateArn,
    });
    expect(config.domain?.alternativeNames).toEqual(['www.saga.example.com', 'review.example.com']);
  });

  test('requires a certificate when a domain is set', () => {
    expect(() => resolve({ [CONTEXT_KEYS.domainName]: 'saga.example.com' })).toThrow(
      /certificateArn.*is required/s,
    );
  });

  test('rejects a certificate outside us-east-1', () => {
    expect(() =>
      resolve({
        [CONTEXT_KEYS.domainName]: 'saga.example.com',
        [CONTEXT_KEYS.certificateArn]:
          'arn:aws:acm:eu-west-1:111122223333:certificate/6b1f0a3c-1c0e-4c9f-9a8a-2f5f0b4c1d2e',
      }),
    ).toThrow(/us-east-1/);
  });

  test('rejects domain settings supplied without a domain name', () => {
    expect(() => resolve({ [CONTEXT_KEYS.certificateArn]: certificateArn })).toThrow(
      /requires "reviewSaga:domainName"/,
    );
    expect(() => resolve({ [CONTEXT_KEYS.hostedZoneId]: 'Z123456' })).toThrow(
      /requires "reviewSaga:domainName"/,
    );
  });

  test('requires hosted zone id and name together', () => {
    expect(() =>
      resolve({
        [CONTEXT_KEYS.domainName]: 'saga.example.com',
        [CONTEXT_KEYS.certificateArn]: certificateArn,
        [CONTEXT_KEYS.hostedZoneId]: 'Z0123456789ABCDEFGHIJ',
      }),
    ).toThrow(/must be provided together/);
  });

  test('rejects a domain that is outside the hosted zone', () => {
    expect(() =>
      resolve({
        [CONTEXT_KEYS.domainName]: 'saga.example.com',
        [CONTEXT_KEYS.certificateArn]: certificateArn,
        [CONTEXT_KEYS.hostedZoneId]: 'Z0123456789ABCDEFGHIJ',
        [CONTEXT_KEYS.hostedZoneName]: 'example.net',
      }),
    ).toThrow(/is not inside hosted zone/);
  });

  test('accepts a domain inside its hosted zone', () => {
    const config = resolve({
      [CONTEXT_KEYS.domainName]: 'saga.example.com',
      [CONTEXT_KEYS.certificateArn]: certificateArn,
      [CONTEXT_KEYS.hostedZoneId]: 'Z0123456789ABCDEFGHIJ',
      [CONTEXT_KEYS.hostedZoneName]: 'example.com',
    });
    expect(config.domain?.hostedZoneId).toBe('Z0123456789ABCDEFGHIJ');
  });
});
