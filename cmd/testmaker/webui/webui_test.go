package webui_test

import (
	"io/fs"
	"testing"

	"github.com/mariotoffia/testmaker/cmd/testmaker/webui"
)

func TestFSWithoutBuildReportsNotOK(t *testing.T) {
	sub, ok := webui.FS()
	if sub == nil {
		t.Fatal("FS() filesystem must never be nil")
	}
	// A checkout without `make webui` has only the committed placeholder, so
	// no index.html and ok must be false. (After a local build this test still
	// passes the nil/consistency checks below and skips the not-ok assertion.)
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		if ok {
			t.Fatal("ok must be false when dist has no index.html")
		}
	} else if !ok {
		t.Fatal("ok must be true when dist/index.html exists")
	}
}
