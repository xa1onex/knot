package edge

import (
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

// Stage 7.3 — Edge tunnel streaming limits (Control Plane enforces; agent mirrors).
const (
	ChunkBytes           = protocol.EdgeChunkBytes
	MaxRequestBytes      = protocol.MaxEdgeRequestBytes
	MaxBufferPerStream   = protocol.MaxEdgeBufferPerStream
	MaxInflightChunks    = protocol.MaxEdgeInflightChunks
	MaxConcurrentDevice  = protocol.MaxEdgeConcurrentPerDevice
	MaxConcurrentService = protocol.MaxEdgeConcurrentPerService
	DefaultReqTimeout    = 5 * time.Minute
	DefaultIdleTimeout   = 2 * time.Minute
)
