# Contributing

Thank you for your interest in contributing to the Elasticsearch data source for Grafana! We welcome contributions from the community.

Feel free to [browse open issues](https://github.com/grafana/grafana-elasticsearch-datasource/issues) or open a new one. For more general guidance, see [Grafana's Contributing Guide](https://github.com/grafana/grafana/blob/main/CONTRIBUTING.md).

This project adheres to the [Grafana Code of Conduct](https://github.com/grafana/grafana/blob/main/CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## Prerequisites

- [Git](https://git-scm.com/)
- [Go](https://golang.org/dl/) (see [go.mod](go.mod) for the minimum required version)
- [Mage](https://magefile.org/)
- [Node.js LTS](https://nodejs.org)
- [npm](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm) (see [package.json](package.json) for the minimum required version)
- [Docker](https://docs.docker.com/get-docker/)

## Frontend

1. Install dependencies:

   ```shell
   npm install
   ```

2. Build plugin in development mode and watch for changes:

   ```shell
   npm run dev
   ```

3. Build plugin in production mode:

   ```shell
   npm run build
   ```

4. Run frontend tests:

   ```shell
   npm run test:ci
   ```

## Backend

1. Build the backend binaries:

   ```shell
   mage -v
   ```

## Local development environment

`npm run server` starts a single-node Elasticsearch instance and a Grafana instance with the plugin pre-provisioned:

```shell
npm run server
```

To start a specific Elasticsearch version:

```shell
ELASTICSEARCH_VERSION=8.17.0 npm run server
```

## E2E tests

```shell
npm run server
npm run e2e
```

Or, to install Playwright browsers first:

```shell
npx playwright install --with-deps
npm run server
npm run e2e
```

## Release

You need commit access to the repository to publish a release.

1. Update the version number in `package.json`.
2. Update `CHANGELOG.md` with the changes included in the release.
3. Open a PR with the changes and merge it.
4. Follow the release process described [here](https://enghub.grafana-ops.net/docs/default/component/grafana-plugins-platform/plugins-ci-github-actions/010-plugins-ci-github-actions/#cd_1).
