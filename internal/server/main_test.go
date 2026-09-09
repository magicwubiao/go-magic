package server

import (
	"fmt"
	"os"
	"testing"
)

// TestMain isolates every test in this package from the developer's real
// magic home. Handler tests construct partial Config structs (e.g.
// &appconfig.Config{Providers: {...}}), and some handlers persist them.
// Without this redirection, running `go test ./internal/server/...` used to
// OVERWRITE the real ~/.magic/config.json with test data — the recurring
// "配置丢失/被还原" root cause (e.g. a leftover providers.custom with
// api_key "sk-new" / models ["m1"] from TestProviderUpdateVision).
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "magic-server-tests-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create temp magic home for tests:", err)
		os.Exit(1)
	}
	os.Setenv("GO_MAGIC_HOME", tmp)
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
