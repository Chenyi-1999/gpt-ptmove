//go:build linux

package plugins

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func restrictPackageWorker() error {
	// Go reserves part of its virtual address space before main starts.
	// Measure that reservation before the worker reads the guest.
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return fmt.Errorf("读取插件工作进程的资源基线: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 6 {
		return errors.New("插件工作进程的资源基线无效")
	}
	addressPages, err1 := strconv.ParseUint(fields[0], 10, 64)
	dataPages, err2 := strconv.ParseUint(fields[5], 10, 64)
	if err1 != nil || err2 != nil || addressPages == 0 || dataPages == 0 {
		return errors.New("插件工作进程的资源基线无效")
	}
	page := uint64(os.Getpagesize())
	for _, budget := range []struct {
		resource int
		limit    uint64
	}{
		{unix.RLIMIT_AS, addressPages*page + (256 << 20)},
		{unix.RLIMIT_DATA, dataPages*page + (160 << 20)},
		{unix.RLIMIT_CPU, 6},
		{unix.RLIMIT_CORE, 0},
	} {
		var current unix.Rlimit
		if err := unix.Getrlimit(budget.resource, &current); err != nil {
			return fmt.Errorf("读取插件工作进程的资源限制: %w", err)
		}
		cap := min(budget.limit, current.Cur, current.Max)
		if err := unix.Setrlimit(budget.resource, &unix.Rlimit{Cur: cap, Max: cap}); err != nil {
			return fmt.Errorf("设置插件工作进程的资源限制: %w", err)
		}
	}
	return nil
}
