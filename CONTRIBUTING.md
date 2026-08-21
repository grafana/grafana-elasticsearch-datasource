# Contributing

## Signed commits are required

> [!IMPORTANT]
> All commits must be [signed](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits) (GPG, SSH, or S/MIME) to be merged into this repository. Pull requests with unsigned commits will need to be re-committed with signatures before they can be merged.

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

Releases are automated with [release-please](https://github.com/googleapis/release-please). The version number and the changelog both come from commit messages, so there is nothing to edit by hand.

### What you do

Title your pull request as a [Conventional Commit](https://www.conventionalcommits.org/). The `PR Title` check enforces this, and because the repository squash-merges, the PR title becomes the commit subject that release-please reads.

| Prefix | Effect on the next release |
| --- | --- |
| `fix:` | patch version |
| `feat:` | minor version |
| `feat!:`, or a `BREAKING CHANGE:` footer | major version |
| `chore:` | no release, hidden from the changelog |
| `docs:`, `test:`, `build:`, `ci:`, `refactor:`, `perf:`, `revert:` | no version bump, listed in the changelog |

### What happens next

1. release-please opens a `chore(main): release X.Y.Z` pull request and keeps it up to date as more commits land.
2. Merging that pull request creates the tag and the GitHub release, and publishes the plugin to the prod catalog.
3. Every other push to `main` publishes to the dev catalog instead.

### Do not

Do not edit the version in `package.json`, and do not write `CHANGELOG.md` entries by hand. release-please owns both files, and a manual edit puts `package.json` out of step with `.release-please-manifest.json`, which makes the next release pick a wrong version.

To release a specific version, add a `Release-As: X.Y.Z` footer to a commit rather than editing the version.
