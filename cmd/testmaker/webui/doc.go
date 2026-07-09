// Package webui embeds the built web application (operator console + test
// player; DESIGN.md §7.1, ADR-0005). The dist directory is produced by
// `make webui` (web/ source built by Vite); the committed dist/.keep
// placeholder keeps the go:embed pattern valid on a checkout with no UI
// build, in which case FS reports ok=false and the delivery surface falls
// back to the JSON index.
package webui
