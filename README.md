# Elasticsearch data source for Grafana

> **Note**: This core plugin was extracted from the [grafana/grafana](https://github.com/grafana/grafana) repository
> and is now bundled with Grafana.

## Overview

Elasticsearch data source for Grafana — query and visualize metrics, logs, and traces stored in Elasticsearch.

## Requirements

- Grafana 12.2.0 or later
- Elasticsearch 8.x or 9.x (7.x is past its [end of life](https://www.elastic.co/support/eol) and no longer supported)

## Getting started

This plugin is bundled with Grafana — no installation is required for standard Grafana deployments.

1. Navigate to **Connections > Data sources** in Grafana.
2. Click **Add data source** and search for "Elasticsearch".
3. Configure the connection settings and click **Save & test**.

For detailed setup instructions, see the
[Elasticsearch data source documentation](https://grafana.com/docs/grafana/latest/datasources/elasticsearch/).

### Custom Grafana distributions

If you are building a custom Grafana binary or distribution that excludes bundled plugins,
you can install this plugin from the [Grafana plugin catalog](https://grafana.com/grafana/plugins/).

## Documentation

Full documentation is available at:

https://grafana.com/docs/grafana/latest/datasources/elasticsearch/

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

This plugin is licensed under the [AGPL-3.0](LICENSE).
