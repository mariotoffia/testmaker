package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mariotoffia/testmaker/domain/clock"
)

// TestFullTakePathTwoRoles walks operator setup → invite → taker attempt →
// score, asserting the role boundary holds at every step. It is the executable
// contract behind DESIGN §7.3's sequence diagram.
func TestFullTakePathTwoRoles(t *testing.T) {
	db, err := openTestDB("memory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.close() })
	blobs, _ := openBlobStore("memory")
	cfg := AuthConfig{Mode: "token", OperatorToken: "OP", Secret: "SECRET", InviteTTLSeconds: 3600}
	ts := httptest.NewServer(newServer(serverDeps{db: db, blobs: blobs, authCfg: cfg, clock: clock.System()}).routes())
	t.Cleanup(ts.Close)

	// Operator seeds bank + test.
	if r := authPost(t, ts.URL+"/api/items/generate", "OP", generateReq{TestType: "A2", Difficulty: 1, Count: 6, Seed: 7}); r.StatusCode != http.StatusOK {
		t.Fatalf("generate → %d", r.StatusCode)
	}
	if r := authPost(t, ts.URL+"/api/tests", "OP", composeReq{
		ID: "quiz", Title: "Quiz", Policy: "fixed-increasing",
		Sections: []sectionReq{{Title: "L", Family: "logical", MinDifficulty: 1, MaxDifficulty: 3, PerItemSeconds: 60}},
	}); r.StatusCode != http.StatusCreated {
		t.Fatalf("compose → %d", r.StatusCode)
	}

	// Operator mints an invite; taker previews + starts.
	var inv struct{ Token string }
	decodeBody(t, authPost(t, ts.URL+"/api/tests/quiz/invites", "OP", nil), &inv)
	if r := authGet(t, ts.URL+"/api/invites/preview", inv.Token); r.StatusCode != http.StatusOK {
		t.Fatalf("preview → %d", r.StatusCode)
	}
	var start struct {
		Session struct {
			ID        string
			Presented struct{ ItemID string }
		}
		Item         *struct{ AnswerKey struct{ OptionID string } }
		SessionToken string
	}
	decodeBody(t, authPost(t, ts.URL+"/api/invites/start", inv.Token, nil), &start)

	// The taker's presented item must be key-redacted (executor strips the key).
	if start.Item != nil && start.Item.AnswerKey.OptionID != "" {
		t.Fatal("presented item leaked its answer key to the taker")
	}

	// Taker answers every presented item with the session token, then completes.
	sid, stok := start.Session.ID, start.SessionToken
	presented := start.Session.Presented.ItemID
	for presented != "" {
		var d struct {
			Session struct {
				Presented struct{ ItemID string }
			}
		}
		decodeBody(t, authPost(t, ts.URL+"/api/sessions/"+sid+"/answers", stok,
			answerReq{ItemID: presented, OptionID: "a"}), &d)
		presented = d.Session.Presented.ItemID
	}
	if r := authPost(t, ts.URL+"/api/sessions/"+sid+"/complete", stok, nil); r.StatusCode != http.StatusOK {
		t.Fatalf("complete → %d", r.StatusCode)
	}

	// Taker reads their score; an anonymous caller cannot.
	if r := authGet(t, ts.URL+"/api/sessions/"+sid+"/score", stok); r.StatusCode != http.StatusOK {
		t.Fatalf("score with session token → %d, want 200", r.StatusCode)
	}
	if r := authGet(t, ts.URL+"/api/sessions/"+sid+"/score", ""); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon score → %d, want 401", r.StatusCode)
	}

	// The operator bank view (keys) stays operator-only throughout.
	if r := authGet(t, ts.URL+"/api/items", stok); r.StatusCode != http.StatusForbidden {
		t.Fatalf("taker hitting operator bank → %d, want 403", r.StatusCode)
	}
}
