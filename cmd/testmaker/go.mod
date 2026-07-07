module github.com/mariotoffia/testmaker/cmd/testmaker

go 1.25.0

require (
	github.com/mariotoffia/testmaker v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/source/filecatalog v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog v0.0.0
)

require gopkg.in/yaml.v3 v3.0.1 // indirect

replace github.com/mariotoffia/testmaker => ../..

replace github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher => ../../adapters/native/fetch/stubfetcher

replace github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat => ../../adapters/native/llm/openaicompat

replace github.com/mariotoffia/testmaker/testutil/ollamalocal => ../../testutil/ollamalocal

replace github.com/mariotoffia/testmaker/adapters/native/source/filecatalog => ../../adapters/native/source/filecatalog

replace github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog => ../../adapters/native/source/memorycatalog
