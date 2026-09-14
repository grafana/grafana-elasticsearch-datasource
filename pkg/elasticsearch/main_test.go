package elasticsearch

import (
	"context"
	"os"
	"testing"
)

// TestMain keeps the package's unit tests off the network. The dataplane gate
// is a feature flag evaluated over HTTP, and its default endpoint resolves
// only inside the cluster, so an unstubbed evaluation would pay the dial
// timeout once per process and log a warning.
func TestMain(m *testing.M) {
	isDataplaneEnabled = func(context.Context) bool { return false }
	os.Exit(m.Run())
}
