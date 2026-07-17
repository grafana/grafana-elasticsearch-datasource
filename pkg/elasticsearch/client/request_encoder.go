package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

// requestEncoder handles encoding of search requests to Elasticsearch format
type requestEncoder struct {
	logger log.Logger
}

// newRequestEncoder creates a new request encoder
func newRequestEncoder(logger log.Logger) *requestEncoder {
	return &requestEncoder{
		logger: logger,
	}
}

// encodeBatchRequests encodes multiple requests into NDJSON format
func (e *requestEncoder) encodeBatchRequests(requests []*multiRequest) ([]byte, error) {
	start := time.Now()
	defer func() {
		e.logger.Debug("Completed encoding of batch requests to json", "duration", time.Since(start))
	}()

	payload := bytes.Buffer{}
	for _, r := range requests {
		reqHeader, err := json.Marshal(r.header)
		if err != nil {
			return nil, err
		}
		payload.WriteString(string(reqHeader) + "\n")

		body := ""
		switch r.body.(type) {
		case *SearchRequest:
			reqBody, err := json.Marshal(r.body)
			if err != nil {
				return nil, err
			}
			body = string(reqBody)
		case string:
			body = r.body.(string)
		default:
			return nil, fmt.Errorf("unknown request type: %T", r.body)
		}

		body, err = interpolateSearchBody(body, r.interval)
		if err != nil {
			return nil, fmt.Errorf("interpolating macros in search request body: %w", err)
		}

		payload.WriteString(body + "\n")
	}

	return payload.Bytes(), nil
}
