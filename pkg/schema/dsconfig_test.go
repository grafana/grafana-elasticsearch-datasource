package schema_test

import (
	_ "embed"
	"testing"

	"github.com/grafana/dsconfig/schema"
	"github.com/grafana/grafana-elasticsearch-datasource/pkg/schema/models"
)

//go:embed dsconfig.json
var configSchemaJSON []byte

//go:generate go test -run TestPlugin -generateArtifacts
func TestPlugin(t *testing.T) {
	schema.RunPluginTests(t, schema.PluginUnderTest{
		ID:                "elasticsearch",
		ConfigSchemaJSON:  configSchemaJSON,
		SettingsJSONModel: models.DatasourceSettings{},
		SecureKeys: []string{
			"basicAuthPassword", "apiKey", "tlsCACert", "tlsClientCert", "tlsClientKey",
			"sigV4AccessKey", "sigV4SecretKey",
		},
	})
}
