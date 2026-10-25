package archive

import "github.com/moby/go-archive/compression"

// Uncompressed preserves the API used by act v0.2.89 when creating Docker build contexts.
const Uncompressed = compression.None
