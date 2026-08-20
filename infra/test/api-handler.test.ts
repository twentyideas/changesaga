import { handler } from '../lambda/health/index';

const ORIGINAL_ENV = { ...process.env };

function request(method: string, rawPath: string) {
  return {
    version: '2.0',
    rawPath,
    requestContext: { requestId: 'test-request-id', http: { method, path: rawPath } },
  };
}

beforeEach(() => {
  process.env = {
    ...ORIGINAL_ENV,
    APP_NAME: 'review-saga',
    APP_ENVIRONMENT: 'test',
    APP_VERSION: '1.2.3',
  };
  delete process.env.GITHUB_APP_SECRET_ARN;
  delete process.env.GITHUB_APP_ID;
  delete process.env.SITE_URL;
});

afterAll(() => {
  process.env = ORIGINAL_ENV;
});

describe('GET /api/health', () => {
  test('reports the deployed environment and version', async () => {
    const response = await handler(request('GET', '/api/health'));
    expect(response.statusCode).toBe(200);
    expect(response.headers['cache-control']).toBe('no-store');

    const body = JSON.parse(response.body);
    expect(body.status).toBe('ok');
    expect(body.environment).toBe('test');
    expect(body.version).toBe('1.2.3');
    expect(body.requestId).toBe('test-request-id');
    expect(Date.parse(body.time)).not.toBeNaN();
  });

  test('tolerates a trailing slash', async () => {
    expect((await handler(request('GET', '/api/health/'))).statusCode).toBe(200);
  });
});

describe('GET /api/config', () => {
  test('reports every unfinished capability as disabled', async () => {
    const body = JSON.parse((await handler(request('GET', '/api/config'))).body);
    expect(body.apiBasePath).toBe('/api');
    expect(body.sagaSource).toBe('none');
    expect(body.features).toEqual({
      githubApp: false,
      commentPosting: false,
      privateRepositories: false,
    });
    expect(body.siteUrl).toBeNull();
  });

  test('reports the GitHub App as configured without revealing the secret', async () => {
    process.env.GITHUB_APP_SECRET_ARN =
      'arn:aws:secretsmanager:eu-west-1:111122223333:secret:review-saga/github-app-AbCdEf';
    process.env.GITHUB_APP_ID = '123456';

    const response = await handler(request('GET', '/api/config'));
    const body = JSON.parse(response.body);
    expect(body.features.githubApp).toBe(true);
    // Even when configured, comment posting stays off until it is implemented.
    expect(body.features.commentPosting).toBe(false);
    expect(response.body).not.toContain('arn:aws:secretsmanager');
  });
});

describe('routing', () => {
  test('unknown paths return a JSON 404', async () => {
    const response = await handler(request('GET', '/api/comments'));
    expect(response.statusCode).toBe(404);
    expect(JSON.parse(response.body).error).toBe('not_found');
  });

  test('the wrong method returns 405 with an Allow header', async () => {
    const response = await handler(request('POST', '/api/health'));
    expect(response.statusCode).toBe(405);
    expect(response.headers.allow).toBe('GET');
  });

  test('a request with no path information does not throw', async () => {
    const response = await handler({});
    expect(response.statusCode).toBe(404);
  });
});
