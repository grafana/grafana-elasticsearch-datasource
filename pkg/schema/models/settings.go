package models

// DatasourceSettings mirrors the jsonData keys of the schema. Test-only; the backend parses jsonData into a map.
type DatasourceSettings struct {
	// Keys read by the plugin SDK, not by plugin code.
	KeepCookies            []string `json:"keepCookies"`
	Timeout                float64  `json:"timeout"`
	TLSAuth                bool     `json:"tlsAuth"`
	TLSAuthWithCACert      bool     `json:"tlsAuthWithCACert"`
	ServerName             string   `json:"serverName"`
	TLSSkipVerify          bool     `json:"tlsSkipVerify"`
	OAuthPassThru          bool     `json:"oauthPassThru"`
	SigV4Auth              bool     `json:"sigV4Auth"`
	SigV4AuthType          string   `json:"sigV4AuthType"`
	SigV4Profile           string   `json:"sigV4Profile"`
	SigV4AssumeRoleArn     string   `json:"sigV4AssumeRoleArn"`
	SigV4ExternalID        string   `json:"sigV4ExternalId"`
	SigV4Region            string   `json:"sigV4Region"`
	EnableSecureSocksProxy bool     `json:"enableSecureSocksProxy"`

	APIKeyAuth                 bool   `json:"apiKeyAuth"`
	Index                      string `json:"index"`
	Interval                   string `json:"interval"`
	TimeField                  string `json:"timeField"`
	MaxConcurrentShardRequests int64  `json:"maxConcurrentShardRequests"`
	IncludeFrozen              bool   `json:"includeFrozen"`
	LogMessageField            string `json:"logMessageField"`
	LogLevelField              string `json:"logLevelField"`

	// Frontend-only; the backend never reads these.
	TimeInterval     string `json:"timeInterval"`
	DefaultQueryMode string `json:"defaultQueryMode"`
	DataLinks        []any  `json:"dataLinks"`
}
