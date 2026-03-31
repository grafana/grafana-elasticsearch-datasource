# Changelog

## 12.5.2

- Feature: Add support for runtime fields [#189](https://github.com/grafana/grafana-elasticsearch-datasource/pull/189)
- Docs: Add README and CONTRIBUTING guide [#212](https://github.com/grafana/grafana-elasticsearch-datasource/pull/212)
- Dependency updates:
  - Chore: Update dependency @grafana/data to v13.0.0-23796392586 [#211](https://github.com/grafana/grafana-elasticsearch-datasource/pull/211) and previous versions
  - Chore: Update grafana monorepo [#173](https://github.com/grafana/grafana-elasticsearch-datasource/pull/173)
  - Chore: Update dependency @elastic/esql to v1.6.0 [#179](https://github.com/grafana/grafana-elasticsearch-datasource/pull/179)
  - Chore: Update dependency @swc/core to ^1.15.18 [#172](https://github.com/grafana/grafana-elasticsearch-datasource/pull/172)
  - Chore: Update dependency @swc/helpers to ^0.5.19 [#199](https://github.com/grafana/grafana-elasticsearch-datasource/pull/199)
  - Chore: Update npm to v11.12.1 [#200](https://github.com/grafana/grafana-elasticsearch-datasource/pull/200)

## 12.5.1

- Fix: Correctly support legacy template variables [#162](https://github.com/grafana/grafana-elasticsearch-datasource/pull/162)
- Fix: Raw query editor orderBy bug [#161](https://github.com/grafana/grafana-elasticsearch-datasource/pull/161)

## 12.5.0

- Feature: Add support for ES|QL queries [#124](https://github.com/grafana/grafana-elasticsearch-datasource/pull/124)
- Fix: Explicitly forward Content-Type header to upstream requests [#133](https://github.com/grafana/grafana-elasticsearch-datasource/pull/133)

## 12.4.3

- Fix: Add missing AWS authentication middleware
- Chore: Copy query editor options box from core [#104](https://github.com/grafana/grafana-elasticsearch-datasource/pull/104)
- Chore: Copy variable query editor support from core [#100](https://github.com/grafana/grafana-elasticsearch-datasource/pull/100)

## 12.4.2

- Initial release of the Elasticsearch data source as an external data source.
