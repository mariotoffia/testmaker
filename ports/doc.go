// Package ports declares the hexagon boundary of Testmaker: the interfaces that
// the application core calls out through (driven ports) and that drive it
// (driving ports). It imports the domain only — never app or adapters.
//
// Driven ports (core calls out):
//   - SourceCatalog / SourceRepository : read / read-write source catalogue
//   - Fetcher                          : pull raw items from a source
//   - ItemRepository                   : persist item-bank items          (scaffold)
//   - TestRepository                   : persist composed tests           (scaffold)
//   - SessionRepository                : persist test-taking sessions     (scaffold)
//   - Generator                        : procedurally generate items      (scaffold)
//   - Scorer                           : score a completed session        (scaffold)
//
// Driving ports (drive the core):
//   - CatalogLoader                    : ingest a catalogue file
//   - Executor                         : administer a test                (scaffold)
//
// DTOs cross these ports as domain Snapshots, never as aggregates.
package ports
