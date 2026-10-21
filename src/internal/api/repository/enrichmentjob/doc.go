// Package enrichmentjob provides the PostgreSQL repository the API uses to
// enqueue enrichment jobs. Claiming, retrying and cleaning up jobs is the
// enricher's responsibility and lives in mnemonic-enricher.
//
// Documentation:

// - Architecture: https://github.com/twistingmercury/librarian-docs/blob/develop/docs/architecture/dbs/08-data-architecture.md (Data Model Design > Enrichment Jobs, Consistency and Integrity, Data Lifecycle Management)
// - Design: https://github.com/twistingmercury/librarian-docs/blob/develop/docs/architecture/system/04-data-architecture.md (Repository Interfaces > EnrichmentJob Repository)
// - Design: https://github.com/twistingmercury/librarian-docs/blob/develop/docs/design/pattern-processing.md (Enrichment Pipeline, Enrichment Worker Deployment)
package enrichmentjob
