//go:build darwin

package main

import "errors"

var errUninstallUnsupported = errors.New("uninstall is only supported on windows")

// RunUninstall macOS 卸载暂未实现：防卸载 Go 化仅覆盖 Windows 生产布局
func RunUninstall(codeFlag string) error {
	return errUninstallUnsupported
}
