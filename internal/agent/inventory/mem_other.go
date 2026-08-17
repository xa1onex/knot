//go:build !linux && !darwin && !windows

package inventory

import "github.com/knot-infra/knot/pkg/protocol"

func readMemory() protocol.ComputeMemory { return protocol.ComputeMemory{} }
