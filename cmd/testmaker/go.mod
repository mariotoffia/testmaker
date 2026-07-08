module github.com/mariotoffia/testmaker/cmd/testmaker

go 1.25.0

require (
	github.com/mariotoffia/testmaker v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/source/filecatalog v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb v0.0.0
	github.com/mariotoffia/testmaker/adapters/native/testdb/sqlitetestdb v0.0.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.42.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.51.0 // indirect
)

replace github.com/mariotoffia/testmaker => ../..

replace github.com/mariotoffia/testmaker/adapters/native/fetch/stubfetcher => ../../adapters/native/fetch/stubfetcher

replace github.com/mariotoffia/testmaker/adapters/native/llm/openaicompat => ../../adapters/native/llm/openaicompat

replace github.com/mariotoffia/testmaker/testutil/ollamalocal => ../../testutil/ollamalocal

replace github.com/mariotoffia/testmaker/adapters/native/source/filecatalog => ../../adapters/native/source/filecatalog

replace github.com/mariotoffia/testmaker/adapters/native/source/memorycatalog => ../../adapters/native/source/memorycatalog

replace github.com/mariotoffia/testmaker/adapters/native/testdb/memorytestdb => ../../adapters/native/testdb/memorytestdb

replace github.com/mariotoffia/testmaker/adapters/native/testdb/sqlitetestdb => ../../adapters/native/testdb/sqlitetestdb
