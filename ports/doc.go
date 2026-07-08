// Package ports declares the hexagon boundary of Testmaker: the interfaces that
// the application core calls out through (driven ports) and that drive it
// (driving ports). It imports the domain only — never app or adapters.
//
// Driven ports (core calls out):
//   - SourceRepository                  : read/write source catalogue
//   - CatalogLoader                    : ingest a catalogue file
//   - Fetcher                          : pull raw items from a source
//   - ItemRepository                   : persist item-bank items          (scaffold)
//   - TestRepository                   : persist composed tests           (scaffold)
//   - SessionRepository                : persist test-taking sessions     (scaffold)
//   - Generator                        : procedurally generate items      (scaffold)
//   - LLM                              : language-model completion for extraction / translation / derivation steps
//   - PromptRepository                 : versioned prompt templates the LLM service auto-applies
//
// Driving ports (drive the core):
//   - Executor                         : administer a test                (scaffold)
//   - Scorer                           : score a completed session
//
// DTOs cross these ports as domain Snapshots, never as aggregates.
package ports
