package e2e

import (
	"os"
	"testing"

	"github.com/jspdown/dashboard/e2e/internal/pgtest"
)

// TestMain owns the test binary's shared resources: the Postgres
// container (started lazily) and the headless Chrome process. Per-test
// resources (fresh database, fake GitHub server, tab) come from
// e2e.Start.
//
// Tests skip cleanly when Postgres or Chrome can't start; TestMain
// still completes and reports zero failures so CI can tell "skipped due
// to environment" from "test failure".
func TestMain(m *testing.M) {
	code := m.Run()
	stopShared()
	pgtest.Cleanup()
	os.Exit(code)
}
