package main

import (
	"net/http"
	"time"

	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
	"github.com/mariotoffia/testmaker/ports"
)

// mintInviteReq optionally shortens the invite lifetime; 0/absent → config TTL.
type mintInviteReq struct {
	ExpiresInSeconds int `json:"expiresInSeconds"`
}

type inviteResponse struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// sectionSummary is the taker-safe view of a section: counts and timing only,
// never item ids or difficulty bands (those would hand a taker the test).
type sectionSummary struct {
	Title          string `json:"title"`
	Family         string `json:"family"`
	ItemCount      int    `json:"itemCount"`
	TotalSeconds   int    `json:"totalSeconds"`
	PerItemSeconds int    `json:"perItemSeconds"`
}

type invitePreview struct {
	TestID         string           `json:"testId"`
	Title          string           `json:"title"`
	Policy         string           `json:"policy"`
	TotalSeconds   int              `json:"totalSeconds"`
	PerItemSeconds int              `json:"perItemSeconds"`
	ItemCount      int              `json:"itemCount"`
	Sections       []sectionSummary `json:"sections"`
	ExpiresAt      time.Time        `json:"expiresAt"`
}

// startResponse embeds the executor's Delivery (PascalCase, marshalled as-is)
// and adds the session capability token the taker uses for the rest of the
// attempt. Operator direct-start returns the same shape.
type startResponse struct {
	ports.Delivery
	SessionToken string `json:"SessionToken"`
}

// handleMintInvite (operator) issues a signed, expiring invite for a test after
// confirming the test exists.
func (s *server) handleMintInvite(w http.ResponseWriter, r *http.Request) {
	id := testset.TestID(r.PathValue("id"))
	if _, err := s.tests.GetTest(r.Context(), id); err != nil {
		s.writeError(w, r, err)
		return
	}
	var req mintInviteReq
	if !s.decodeOptionalJSON(w, r, &req) {
		return
	}
	tok, exp, err := s.auth.mintInvite(string(id), time.Duration(req.ExpiresInSeconds)*time.Second)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, inviteResponse{Token: tok, URL: "/take#" + tok, ExpiresAt: exp})
}

// handleInvitePreview (invite) returns the redacted test summary. tid and exp
// come from the verified invite, so a taker can only preview the invited test
// and the preview echoes that invite's real expiry (C7).
func (s *server) handleInvitePreview(w http.ResponseWriter, r *http.Request, tid string, exp time.Time) {
	test, err := s.tests.GetTest(r.Context(), testset.TestID(tid))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	pv := invitePreview{
		TestID: tid, Title: test.Title, Policy: string(test.Policy),
		TotalSeconds: int(test.Timing.Total / time.Second), PerItemSeconds: int(test.Timing.PerItem / time.Second),
		ExpiresAt: exp,
	}
	for _, sec := range test.Sections {
		pv.ItemCount += len(sec.Items)
		pv.Sections = append(pv.Sections, sectionSummary{
			Title: sec.Title, Family: string(sec.Family), ItemCount: len(sec.Items),
			TotalSeconds: int(sec.Timing.Total / time.Second), PerItemSeconds: int(sec.Timing.PerItem / time.Second),
		})
	}
	writeJSON(w, http.StatusOK, pv)
}

// handleInviteStart (invite) starts a session for the invited test and returns
// the opening Delivery plus a fresh session token. The invite expiry is not
// needed once the session exists, so it is ignored here.
func (s *server) handleInviteStart(w http.ResponseWriter, r *http.Request, tid string, _ time.Time) {
	test, err := s.tests.GetTest(r.Context(), testset.TestID(tid))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.startAndRespond(w, r, test)
}

// startAndRespond runs the executor's Start and writes the token-augmented
// Delivery. Shared by invite start and operator direct start.
func (s *server) startAndRespond(w http.ResponseWriter, r *http.Request, test testset.TestSnapshot) {
	d, err := s.exec.Start(r.Context(), test)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tok, err := s.sessionToken(d.Session.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, startResponse{Delivery: d, SessionToken: tok})
}

// sessionToken mints a session capability, or returns "" in none mode (no
// secret) where the token is unused — the surface is open, so the player still
// works by presenting no token.
func (s *server) sessionToken(id session.SessionID) (string, error) {
	if !s.auth.enforced() {
		return "", nil
	}
	return s.auth.mintSession(string(id))
}
