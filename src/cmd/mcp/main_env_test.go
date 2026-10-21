package main

import (
	"fmt"
	"os"
	"testing"
)

// TestMain removes the OpenAI key from the test process, and so from any
// child it starts, because MNEMONIC_ is also the configuration prefix: a key
// exported in the developer's shell would otherwise leak into loaded config.
func TestMain(m *testing.M) {
	if err := os.Unsetenv("MNEMONIC_OPENAI_API_KEY"); err != nil {
		fmt.Fprintln(os.Stderr, "unset MNEMONIC_OPENAI_API_KEY:", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
