//go:build !windows

package main

func tryRunAsService(cfgPath string, force bool) bool {
	return false
}
