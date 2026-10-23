//go:build !windows

package main

import (
	"fmt"
)

func openDoorSocket(handle uintptr) (stream, error) {
	return nil, fmt.Errorf("EleBBS socket transport is supported only on Windows")
}
