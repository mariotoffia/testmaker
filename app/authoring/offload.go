package authoring

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/ports"
)

// offloadMedia moves an item's inline figural media out of the snapshot and into
// the blob store, rewriting each affected MediaRef to the returned content ref.
// A generator (rulegen) emits self-contained "data:" URIs so an item is viewable
// with no store; when a blob store is wired the composition root offloads those
// bytes here, keeping the persisted item small while the renderer resolves the
// ref back through the same port.
//
// It is a no-op (returns snap unchanged) when no store is wired or when no part
// carries an inline data URI — a MediaRef that is already a blob ref or an
// external URL is left untouched, so offload is idempotent.
func (s *Service) offloadMedia(ctx context.Context, snap item.ItemSnapshot) (item.ItemSnapshot, error) {
	if s.blobs == nil {
		return snap, nil
	}

	stimulus := slices.Clone(snap.Stimulus)
	for i := range stimulus {
		ref, ok, err := s.offloadRef(ctx, stimulus[i].MediaRef)
		if err != nil {
			return item.ItemSnapshot{}, err
		}
		if ok {
			stimulus[i].MediaRef = ref
		}
	}

	options := slices.Clone(snap.Options)
	for i := range options {
		ref, ok, err := s.offloadRef(ctx, options[i].MediaRef)
		if err != nil {
			return item.ItemSnapshot{}, err
		}
		if ok {
			options[i].MediaRef = ref
		}
	}

	snap.Stimulus = stimulus
	snap.Options = options
	return snap, nil
}

// offloadRef stores mediaRef in the blob store and returns its content ref when
// mediaRef is an inline base64 data URI; otherwise ok is false and mediaRef is
// left for the caller to keep as-is.
func (s *Service) offloadRef(ctx context.Context, mediaRef string) (ref string, ok bool, err error) {
	ct, data, isData := parseBase64DataURI(mediaRef)
	if !isData {
		return "", false, nil
	}
	ref, err = s.blobs.Put(ctx, ports.Blob{Bytes: data, ContentType: ct})
	if err != nil {
		return "", false, err
	}
	return ref, true, nil
}

// parseBase64DataURI decodes a "data:<content-type>;base64,<payload>" URI (the
// only shape testmaker generators emit). Anything else — a plain URL, an already
// offloaded blob ref, a non-base64 data URI — returns ok=false unchanged.
func parseBase64DataURI(s string) (contentType string, data []byte, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(s, prefix) {
		return "", nil, false
	}
	meta, payload, found := strings.Cut(s[len(prefix):], ",")
	if !found {
		return "", nil, false
	}
	ct, encoding, hasEncoding := strings.Cut(meta, ";")
	if ct == "" || !hasEncoding || encoding != "base64" {
		return "", nil, false
	}
	decoded, derr := base64.StdEncoding.DecodeString(payload)
	if derr != nil || len(decoded) == 0 {
		return "", nil, false
	}
	return ct, decoded, true
}
