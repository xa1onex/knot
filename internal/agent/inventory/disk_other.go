//go:build !linux && !darwin && !windows

package inventory

import "github.com/knot-infra/knot/pkg/protocol"

func readDisks() []protocol.ComputeDisk { return nil }
