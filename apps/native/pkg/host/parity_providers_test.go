package host

import (
	"os"
	"testing"
)

// The parity fixture of the LLM proxy (llmproxy/factory/testdata/parity) is made from the models the Kernel is
// given: with DROI_PARITY_PROVIDERS naming its providers.json, this writes them anew.
func TestKernelProvidersOfTheParityFixture(t *testing.T) {
	path := os.Getenv("DROI_PARITY_PROVIDERS")
	if path == "" {
		t.Skip("DROI_PARITY_PROVIDERS is not set")
	}
	if err := os.WriteFile(path, []byte(kernelProviders("https://api.factory.ai")), 0o644); err != nil {
		t.Fatal(err)
	}
}
