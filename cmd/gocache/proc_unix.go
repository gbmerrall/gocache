//go:build !windows

package main

import "syscall"

func getProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
