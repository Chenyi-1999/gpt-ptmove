//go:build !linux

package plugins

import "errors"

func restrictPackageWorker() error {
	return errors.New("插件工作进程的内存隔离需要 Linux")
}
