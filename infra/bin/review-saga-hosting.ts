#!/usr/bin/env node
import * as path from 'path';
import * as cdk from 'aws-cdk-lib';

import { ConfigError, resolveConfig, resourcePrefix } from '../lib/config';
import { HostingStack } from '../lib/hosting-stack';
import { SiteAssetError } from '../lib/site-assets';

const projectRoot = path.resolve(__dirname, '..');

function main(): void {
  const app = new cdk.App();
  const config = resolveConfig(app.node, { projectRoot });
  const prefix = resourcePrefix(config);

  new HostingStack(app, `${prefix}-hosting`, {
    config,
    description: `Review Saga hosting foundation (${config.environmentName})`,
    // An account/region pair is optional: without one CDK produces an
    // environment-agnostic template that can be deployed anywhere.
    ...(config.account !== undefined && config.region !== undefined
      ? { env: { account: config.account, region: config.region } }
      : {}),
    tags: {
      Application: config.appName,
      Environment: config.environmentName,
      ManagedBy: 'aws-cdk',
    },
  });

  app.synth();
}

try {
  main();
} catch (error) {
  if (error instanceof ConfigError || error instanceof SiteAssetError) {
    // A configuration mistake is a user error, not a stack trace.
    console.error(`\n${error.name}: ${error.message}\n`);
    process.exitCode = 1;
  } else {
    throw error;
  }
}
