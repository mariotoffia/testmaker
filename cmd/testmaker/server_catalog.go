package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/mariotoffia/testmaker/adapters/native/source/filecatalog"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// maxCatalogBody caps a catalogue upload (larger than the 1 MiB default: a full
// research catalogue is a few hundred KB, headroom for growth).
const maxCatalogBody = 4 << 20

// handleUploadCatalog validates an uploaded catalogue JSON body, writes it
// atomically to the configured catalogue path, then reloads it — so the console
// can edit the catalogue, not just re-sync the deploy-time file. A parse/
// validation failure is a 400 and never touches the file (DESIGN §7.2).
func (s *server) handleUploadCatalog(w http.ResponseWriter, r *http.Request) {
	if s.catalogPath == "" {
		s.writeError(w, r, shared.ErrUnsupported.WithMessage("no catalogue path configured"))
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCatalogBody))
	if err != nil {
		s.writeError(w, r, shared.ErrInvalid.WithMessage("catalogue body too large or unreadable"))
		return
	}
	if _, perr := filecatalog.ParseJSON(raw); perr != nil {
		s.writeError(w, r, fmt.Errorf("%w: %s", shared.ErrInvalid, perr))
		return
	}
	// ponytail: no lock around write+sync. atomicWrite's rename keeps the file
	// always-valid, so the worst a concurrent upload can do on this single-tenant
	// operator console is serve the other request's (equally valid) catalogue. Add
	// a per-server mutex if multi-operator writes ever become real.
	if werr := atomicWrite(s.catalogPath, raw); werr != nil {
		s.writeError(w, r, werr)
		return
	}
	n, serr := s.cat.Sync(r.Context())
	if serr != nil {
		s.writeError(w, r, serr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"synced": n})
}

// atomicWrite writes bytes to a temp file in the target's directory and renames
// it over the target, so a crashed or concurrent write never leaves a
// half-written catalogue on disk.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create catalogue dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".catalog-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp catalogue: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp catalogue: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp catalogue: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace catalogue: %w", err)
	}
	return nil
}
