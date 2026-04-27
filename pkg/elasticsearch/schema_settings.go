package elasticsearch

import "time"

const (
	// Fallback table when index-per-table would exceed the cap.
	fallbackTableName = "indexed-documents"
	tableParamIndex   = "index"

	// defaultSchemaMaxIndices caps how many discovered indices are exposed as tables.
	defaultSchemaMaxIndices = 500
)

// schemaSettings holds built-in defaults for dsabstraction / schemads discovery
type schemaSettings struct {
	MaxIndices       int
	IncludeHidden    bool
	IndicesTimeout   time.Duration
	FieldCapsTimeout time.Duration
}

// defaultSchemaSettings returns fixed defaults for SQL abstraction schema discovery.
func defaultSchemaSettings() schemaSettings {
	return schemaSettings{
		MaxIndices:       defaultSchemaMaxIndices,
		IncludeHidden:    false,
		IndicesTimeout:   10 * time.Second,
		FieldCapsTimeout: 15 * time.Second,
	}
}
