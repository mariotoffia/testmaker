package memoryprompts_test

import (
	"testing"

	"github.com/mariotoffia/testmaker/adapters/native/llm/memoryprompts"
	"github.com/mariotoffia/testmaker/ports"
	"github.com/mariotoffia/testmaker/ports/prompttest"
)

// Store satisfies the ports.PromptRepository contract (kept out of the
// production package so it imports no ports package, per the arch rules).
var _ ports.PromptRepository = (*memoryprompts.Store)(nil)

func TestPromptRepositoryConformance(t *testing.T) {
	prompttest.RunPromptRepositoryTests(t, func() ports.PromptRepository {
		return memoryprompts.NewStore()
	})
}
