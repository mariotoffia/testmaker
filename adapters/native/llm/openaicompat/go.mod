module github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat

go 1.25.0

require (
	github.com/mariotoffia/testmaker v0.0.0
	github.com/mariotoffia/testmaker/testutil/ollamalocal v0.0.0
)

replace github.com/mariotoffia/testmaker => ../../../..

replace github.com/mariotoffia/testmaker/testutil/ollamalocal => ../../../../testutil/ollamalocal
