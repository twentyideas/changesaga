import * as path from 'path';
import * as cdk from 'aws-cdk-lib';
import { Template } from 'aws-cdk-lib/assertions';

import { resolveConfig } from '../lib/config';
import { HostingStack } from '../lib/hosting-stack';

export const PROJECT_ROOT = path.resolve(__dirname, '..');
export const REPO_ROOT = path.resolve(__dirname, '..', '..');

/** Builds an app whose context is exactly `context`, ignoring the ambient environment. */
export function synth(context: Record<string, string> = {}): {
  stack: HostingStack;
  template: Template;
} {
  const app = new cdk.App({ context });
  const config = resolveConfig(app.node, { projectRoot: PROJECT_ROOT, env: {} });
  const stack = new HostingStack(app, 'test-hosting', {
    config,
    env: { account: '111122223333', region: 'eu-west-1' },
  });
  return { stack, template: Template.fromStack(stack) };
}
