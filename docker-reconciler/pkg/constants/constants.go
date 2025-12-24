package constants

import "time"

// Image pull timeout - 10 minutes
// This is long enough for large images (1GB+) on moderate connections,
// but short enough to fail fast on network issues.
const ImagePullTimeout = 10 * time.Minute

// Force reconcile image pull timeout - 5 minutes
// Shorter than regular pull timeout to fail fast during force reconcile.
// If image cannot be pulled within this time, force reconcile aborts
// without removing existing containers (preserving service availability).
const ForceReconcileImagePullTimeout = 5 * time.Minute
