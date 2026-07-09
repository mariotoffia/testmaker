package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// seedTestForInvite composes a one-section fixed test via the surface so the
// invite flow has a real test id. Returns the server, operator token, test id.
func seedTestForInvite(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.close() })
	blobs, _ := openBlobStore("memory")
	cfg := AuthConfig{Mode: "token", OperatorToken: "op-tok", Secret: "sec", InviteTTLSeconds: 3600}
	srv := newServer(serverDeps{db: db, blobs: blobs, authCfg: cfg, clock: clock.System()})
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	// Seed one A2 item and compose a test — reuse the generator path.
	authPost(t, ts.URL+"/api/items/generate", "op-tok", generateReq{TestType: "A2", Difficulty: 1, Count: 4, Seed: 1})
	authPost(t, ts.URL+"/api/tests", "op-tok", composeReq{
		ID: "t1", Title: "Demo", Policy: "fixed-increasing",
		Sections: []sectionReq{{Title: "Logic", Family: "logical", MinDifficulty: 1, MaxDifficulty: 3, PerItemSeconds: 60}},
	})
	return ts, "op-tok", "t1"
}

// authPost issues a POST with an optional bearer token (full URL + JSON body —
// the existing post(t, ts, path, body) helper is server-relative and token-blind).
func authPost(t *testing.T, url, token string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return res
}

func TestInviteMintPreviewStartFlow(t *testing.T) {
	ts, op, tid := seedTestForInvite(t)

	// 1. Operator mints an invite.
	res := authPost(t, ts.URL+"/api/tests/"+tid+"/invites", op, map[string]int{"expiresInSeconds": 600})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("mint invite → %d, want 201", res.StatusCode)
	}
	var inv struct{ Token, URL, ExpiresAt string }
	decodeBody(t, res, &inv)
	if inv.Token == "" || inv.URL == "" {
		t.Fatalf("empty invite: %+v", inv)
	}

	// 2. A non-operator cannot mint.
	if r := authPost(t, ts.URL+"/api/tests/"+tid+"/invites", "", nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon mint → %d, want 401", r.StatusCode)
	}

	// 3. Preview with the invite token — no item ids leak.
	pres := authGet(t, ts.URL+"/api/invites/preview", inv.Token)
	if pres.StatusCode != http.StatusOK {
		t.Fatalf("preview → %d, want 200", pres.StatusCode)
	}
	var pv map[string]any
	decodeBody(t, pres, &pv)
	if pv["title"] != "Demo" {
		t.Fatalf("preview title = %v", pv["title"])
	}
	if _, leaked := pv["sections"].([]any)[0].(map[string]any)["items"]; leaked {
		t.Fatal("preview leaked item refs")
	}

	// 4. Start a session with the invite → get a session token.
	sres := authPost(t, ts.URL+"/api/invites/start", inv.Token, nil)
	if sres.StatusCode != http.StatusCreated {
		t.Fatalf("invite start → %d, want 201", sres.StatusCode)
	}
	var start struct {
		Session      struct{ ID string }
		SessionToken string
	}
	decodeBody(t, sres, &start)
	if start.Session.ID == "" || start.SessionToken == "" {
		t.Fatalf("start missing session/token: %+v", start)
	}

	// 5. The session token drives that session; a bare invite token cannot.
	ans := authPost(t, ts.URL+"/api/sessions/"+start.Session.ID+"/answers", start.SessionToken,
		answerReq{ItemID: "does-not-matter", OptionID: "a"})
	if ans.StatusCode == http.StatusUnauthorized || ans.StatusCode == http.StatusForbidden {
		t.Fatalf("session token rejected on its own session: %d", ans.StatusCode)
	}
	if bad := authPost(t, ts.URL+"/api/sessions/"+start.Session.ID+"/answers", inv.Token,
		answerReq{ItemID: "x", OptionID: "a"}); bad.StatusCode != http.StatusForbidden {
		t.Fatalf("invite token on session verb → %d, want 403", bad.StatusCode)
	}
}

// TestInvitePreviewReportsExpiry pins C7: the preview echoes the invite's real
// expiry, not the Go zero time. Regression guard for a dropped exp claim.
func TestInvitePreviewReportsExpiry(t *testing.T) {
	ts, op, tid := seedTestForInvite(t)
	var inv struct {
		Token     string
		ExpiresAt time.Time
	}
	decodeBody(t, authPost(t, ts.URL+"/api/tests/"+tid+"/invites", op, map[string]int{"expiresInSeconds": 600}), &inv)

	var pv struct {
		ExpiresAt time.Time `json:"expiresAt"`
	}
	decodeBody(t, authGet(t, ts.URL+"/api/invites/preview", inv.Token), &pv)
	if pv.ExpiresAt.IsZero() {
		t.Fatal("preview expiresAt is the zero time; C7 requires the real invite expiry")
	}
	if pv.ExpiresAt.Unix() != inv.ExpiresAt.Unix() {
		t.Fatalf("preview expiresAt %v != minted %v", pv.ExpiresAt, inv.ExpiresAt)
	}
}
