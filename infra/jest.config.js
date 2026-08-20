module.exports = {
  testEnvironment: 'node',
  roots: ['<rootDir>/test'],
  testMatch: ['**/*.test.ts'],
  transform: {
    '^.+\\.tsx?$': ['ts-jest', { tsconfig: 'tsconfig.json' }],
  },
  // Each test synthesises a full stack, which is I/O bound rather than CPU
  // bound; parallel workers contend badly and make the suite an order of
  // magnitude slower than running it serially.
  maxWorkers: 1,
  testTimeout: 120000,
};
