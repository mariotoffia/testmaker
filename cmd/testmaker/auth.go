package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
	"github.com/mariotoffia/testmaker/domain/shared"
)

// token type prefixes (C2). A prefix is part of the signed input, so a session
// token can never be replayed as an invite.
const (
	invitePrefix  = "ti"
	sessionPrefix = "ts"
)

// authenticator issues and verifies the three delivery-surface credentials
// (ADR-0006): a static operator token and stateless HMAC-SHA256 invite/session
// capability tokens. It holds no state beyond the config secrets and an
// injected clock (for invite expiry), so any instance verifies any token — the
// property that carries this design into a multi-instance deployment unchanged.
type authenticator struct {
	mode          string
	operatorToken string
	secret        []byte
	inviteTTL     time.Duration
	clk           clock.Clock
}

func newAuthenticator(cfg AuthConfig, clk clock.Clock) *authenticator {
	return &authenticator{
		mode:          cfg.Mode,
		operatorToken: cfg.OperatorToken,
		secret:        []byte(cfg.Secret),
		inviteTTL:     time.Duration(cfg.InviteTTLSeconds) * time.Second,
		clk:           clk,
	}
}

// enforced reports whether role checks apply. "none" mode (and a nil
// authenticator) leaves the surface open for trusted-localhost development.
func (a *authenticator) enforced() bool { return a != nil && a.mode == "token" }

func (a *authenticator) verifyOperator(token string) bool {
	if a.operatorToken == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.operatorToken)) == 1
}

type inviteClaims struct {
	TID string `json:"tid"`
	Exp int64  `json:"exp"`
}

type sessionClaims struct {
	SID string `json:"sid"`
}

func (a *authenticator) sign(prefix, payloadB64 string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(prefix + "." + payloadB64))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *authenticator) mint(prefix string, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", shared.ErrInvalid.Wrap(err).WithMessage("marshal token claims")
	}
	p64 := base64.RawURLEncoding.EncodeToString(payload)
	return prefix + "." + p64 + "." + a.sign(prefix, p64), nil
}

// verify splits a token, checks its prefix, recomputes the HMAC over
// prefix+"."+payload and compares it in constant time, then returns the raw
// payload bytes when the signature holds.
func (a *authenticator) verify(token, prefix string) ([]byte, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != prefix {
		return nil, false
	}
	if !hmac.Equal([]byte(a.sign(prefix, parts[1])), []byte(parts[2])) {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	return payload, true
}

func (a *authenticator) mintInvite(tid string, ttl time.Duration) (string, time.Time, error) {
	if len(a.secret) == 0 {
		return "", time.Time{}, shared.ErrUnsupported.WithMessage(`invites require auth mode "token"`)
	}
	if ttl <= 0 {
		ttl = a.inviteTTL
	}
	exp := a.clk.Now().Add(ttl)
	tok, err := a.mint(invitePrefix, inviteClaims{TID: tid, Exp: exp.Unix()})
	return tok, exp, err
}

// verifyInviteClaims verifies an invite token and returns its full claims (test
// id + expiry). The invite verbs need the expiry too (C7 preview echoes it), so
// this is the primary verifier and verifyInvite is the tid-only convenience.
func (a *authenticator) verifyInviteClaims(token string) (inviteClaims, bool) {
	payload, ok := a.verify(token, invitePrefix)
	if !ok {
		return inviteClaims{}, false
	}
	var c inviteClaims
	if json.Unmarshal(payload, &c) != nil {
		return inviteClaims{}, false
	}
	if a.clk.Now().Unix() >= c.Exp {
		return inviteClaims{}, false
	}
	return c, true
}

func (a *authenticator) verifyInvite(token string) (string, bool) {
	c, ok := a.verifyInviteClaims(token)
	return c.TID, ok
}

func (a *authenticator) mintSession(sid string) (string, error) {
	if len(a.secret) == 0 {
		return "", shared.ErrUnsupported.WithMessage(`session tokens require auth mode "token"`)
	}
	return a.mint(sessionPrefix, sessionClaims{SID: sid})
}

func (a *authenticator) verifySession(token string) (string, bool) {
	payload, ok := a.verify(token, sessionPrefix)
	if !ok {
		return "", false
	}
	var c sessionClaims
	if json.Unmarshal(payload, &c) != nil {
		return "", false
	}
	return c.SID, true
}

// bearer returns the token from an "Authorization: Bearer <token>" header, or
// "" for any other scheme / a missing header (→ anonymous).
func bearer(r *http.Request) string {
	const scheme = "bearer "
	h := r.Header.Get("Authorization")
	if len(h) >= len(scheme) && strings.EqualFold(h[:len(scheme)], scheme) {
		return strings.TrimSpace(h[len(scheme):])
	}
	return ""
}

// requireOperator gates an operator-only handler. Enforced mode: a missing
// token is 401, a present-but-non-operator token is 403. In none mode it is a
// pass-through.
func (s *server) requireOperator(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.enforced() {
			next(w, r)
			return
		}
		tok := bearer(r)
		if tok == "" {
			writeAuthError(w, http.StatusUnauthorized, "auth.required", "authentication required")
			return
		}
		if !s.auth.verifyOperator(tok) {
			writeAuthError(w, http.StatusForbidden, "auth.forbidden", "operator credentials required")
			return
		}
		next(w, r)
	}
}

// requireSession gates a session verb: the caller must hold that session's
// token (sid == path id) or the operator token. None mode passes through.
func (s *server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.enforced() {
			next(w, r)
			return
		}
		tok := bearer(r)
		if tok == "" {
			writeAuthError(w, http.StatusUnauthorized, "auth.required", "authentication required")
			return
		}
		if s.auth.verifyOperator(tok) {
			next(w, r)
			return
		}
		sid, ok := s.auth.verifySession(tok)
		if !ok {
			// A validly-signed invite is a recognized credential on the wrong
			// verb (C3: valid token, wrong role → 403); anything else is a
			// garbage/unrecognized token → 401.
			if _, isInvite := s.auth.verifyInvite(tok); isInvite {
				writeAuthError(w, http.StatusForbidden, "auth.forbidden", "an invite cannot drive a session; start one first")
				return
			}
			writeAuthError(w, http.StatusUnauthorized, "auth.required", "invalid session token")
			return
		}
		if sid != r.PathValue("id") {
			writeAuthError(w, http.StatusForbidden, "auth.forbidden", "token does not match this session")
			return
		}
		next(w, r)
	}
}

// requireInvite gates the invite verbs. It ALWAYS verifies (even in none mode,
// where there is no secret, so it simply 401s — the invite flow is a token-mode
// feature; an operator in none mode starts sessions directly). The verified
// test id is handed to the wrapped handler so it need not re-parse the token.
func (s *server) requireInvite(next func(w http.ResponseWriter, r *http.Request, tid string, exp time.Time)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := s.auth.verifyInviteClaims(bearer(r))
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "auth.required", "a valid invite is required")
			return
		}
		next(w, r, c.TID, time.Unix(c.Exp, 0).UTC())
	}
}

// handleWhoami resolves the presented bearer to a role for the SPA's login
// check. None mode reports everyone as operator (the surface is open).
func (s *server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	role := "anonymous"
	switch {
	case !s.auth.enforced():
		role = "operator"
	case s.auth.verifyOperator(bearer(r)):
		role = "operator"
	default:
		if _, ok := s.auth.verifySession(bearer(r)); ok {
			role = "taker"
		} else if _, ok := s.auth.verifyInvite(bearer(r)); ok {
			role = "taker"
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"role": role, "mode": s.auth.mode})
}
