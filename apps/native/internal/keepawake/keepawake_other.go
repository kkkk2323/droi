//go:build !darwin && !windows

package keepawake

func start() (stop func()) { return func() {} }
