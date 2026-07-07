package memorycatalog_test

import (
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/ports/sourcetest"
)

// Compile-time proof that Store satisfies the port (kept in _test.go so the
// production file imports no ports package, per the arch rules).
var _ ports.SourceRepository = (*memorycatalog.Store)(nil)

func TestSourceRepositoryConformance(t *testing.T) {
	sourcetest.RunSourceRepositoryTests(t, func() ports.SourceRepository {
		return memorycatalog.NewStore()
	})
}
