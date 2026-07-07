package ports

import (
	"context"

	"github.com/mariotoffia/testmaker/domain/item"
	"github.com/mariotoffia/testmaker/domain/session"
	"github.com/mariotoffia/testmaker/domain/testset"
)

// ItemRepository persists item-bank items (driven port).
//
// SCAFFOLD: signatures are provisional and firm up in the Item Bank block.
type ItemRepository interface {
	SaveItem(ctx context.Context, snap item.ItemSnapshot) error
	GetItem(ctx context.Context, id item.ItemID) (item.ItemSnapshot, error)
	ListItems(ctx context.Context, filter item.ItemFilter) ([]item.ItemSnapshot, error)
}

// TestRepository persists composed tests (driven port).
//
// SCAFFOLD: this is "TestDb" from CLAUDE.md; the in-memory and sqlite adapters
// are the first two implementation blocks.
type TestRepository interface {
	SaveTest(ctx context.Context, snap testset.TestSnapshot) error
	GetTest(ctx context.Context, id testset.TestID) (testset.TestSnapshot, error)
	ListTests(ctx context.Context, filter testset.TestFilter) ([]testset.TestSnapshot, error)
	DeleteTest(ctx context.Context, id testset.TestID) error
}

// SessionRepository persists test-taking sessions (driven port).
//
// SCAFFOLD: firms up in the Renderer / Executor block.
type SessionRepository interface {
	SaveSession(ctx context.Context, snap session.SessionSnapshot) error
	GetSession(ctx context.Context, id session.SessionID) (session.SessionSnapshot, error)
}
