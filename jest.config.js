// force timezone to UTC to allow tests to work regardless of local timezone
// generally used by snapshots, but can affect specific tests
process.env.TZ = 'UTC';

const { grafanaESModules, nodeModulesToTransform } = require('./.config/jest/utils');

module.exports = {
  // Jest configuration provided by Grafana scaffolding
  ...require('./.config/jest.config'),
  // ESM-only packages pulled in by @grafana/plugin-ui and @grafana/* 13.3, which jest must transform
  transformIgnorePatterns: [
    nodeModulesToTransform([...grafanaESModules, '@react-hookz/web', '@ver0/deep-equal', '@marcbachmann/cel-js']),
  ],
};
