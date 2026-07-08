package testset

// TestFilter expresses test-repository query criteria. It is an empty shell
// today: every stored test is returned (ListTests sorts by id).
//
// ponytail: no test-query consumer exists yet — authoring queries the *item*
// bank (item.ItemFilter), not the test repo. Add fields (e.g. by family) when a
// query surface (Block 10) actually needs them; a filter no caller populates is
// speculative structure.
type TestFilter struct{}
