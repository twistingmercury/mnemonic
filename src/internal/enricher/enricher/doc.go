// Package enricher runs the enrichment Worker. The Worker processes jobs
// delivered by the queue, claims jobs due for a retry (or never delivered)
// from the database on a short interval, and runs a maintenance loop that
// reclaims stale jobs and deletes old completed and failed jobs. All
// goroutines stop through context cancellation with a bounded drain.
package enricher
