package enricher

import "time"

// SetRetryInterval shortens the retry sweep interval for tests.
func SetRetryInterval(w *Worker, d time.Duration) { w.retryInterval = d }
