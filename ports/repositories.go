package ports

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
)

// ItemRepository persists item-bank items (driven port).
//
// The DTO shells it moves (item.ItemSnapshot, item.ItemFilter) gain fields in
// the Item Bank block; the method set is the firm persistence contract.
type ItemRepository interface {
	// SaveItem inserts or replaces an item by id; an empty id is item.ErrInvalidItem.
	SaveItem(ctx context.Context, snap item.ItemSnapshot) error
	// GetItem returns the item with the given id or item.ErrUnknownItem.
	GetItem(ctx context.Context, id item.ItemID) (item.ItemSnapshot, error)
	// ListItems returns all items matching the filter (empty filter = all).
	ListItems(ctx context.Context, filter item.ItemFilter) ([]item.ItemSnapshot, error)
}

// TestRepository persists composed tests (driven port).
//
// This is "TestDb" from CLAUDE.md; memorytestdb and sqlitetestdb are its
// implementations (Blocks 2–3), both proven against ports/testdbtest.
type TestRepository interface {
	// SaveTest inserts or replaces a test by id; an empty id is testset.ErrInvalidTest.
	SaveTest(ctx context.Context, snap testset.TestSnapshot) error
	// GetTest returns the test with the given id or testset.ErrUnknownTest.
	GetTest(ctx context.Context, id testset.TestID) (testset.TestSnapshot, error)
	// ListTests returns all tests matching the filter (empty filter = all).
	ListTests(ctx context.Context, filter testset.TestFilter) ([]testset.TestSnapshot, error)
	// DeleteTest removes a test by id (no error if absent).
	DeleteTest(ctx context.Context, id testset.TestID) error
}

// SessionRepository persists test-taking sessions (driven port).
//
// The session.SessionSnapshot DTO firms up in the Renderer / Executor block;
// the method set is the firm persistence contract.
type SessionRepository interface {
	// SaveSession inserts or replaces a session by id; an empty id is session.ErrInvalidSession.
	SaveSession(ctx context.Context, snap session.SessionSnapshot) error
	// GetSession returns the session with the given id or session.ErrUnknownSession.
	GetSession(ctx context.Context, id session.SessionID) (session.SessionSnapshot, error)
}
