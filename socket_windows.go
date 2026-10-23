//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	winsock        = syscall.NewLazyDLL("ws2_32.dll")
	winsockStartup = winsock.NewProc("WSAStartup")
	winsockClose   = winsock.NewProc("closesocket")
	winsockIoctl   = winsock.NewProc("ioctlsocket")
	winsockSetSock = winsock.NewProc("setsockopt")
)

type socketConn struct{ handle uintptr }

type processInfo struct {
	pid  uint32
	name string
}

func (c socketConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	buffer := syscall.WSABuf{Len: uint32(len(p)), Buf: &p[0]}
	var received, flags uint32
	for {
		err := syscall.WSARecv(syscall.Handle(c.handle), &buffer, 1, &received, &flags, nil, nil)
		if err == nil {
			if received == 0 {
				return 0, io.EOF
			}
			return int(received), nil
		}
		if retrySocketError(err) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if resetSocketError(err) {
			return 0, io.EOF
		}
		return 0, err
	}
}

func (c socketConn) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		buffer := syscall.WSABuf{Len: uint32(len(p) - written), Buf: &p[written]}
		var sent uint32
		err := syscall.WSASend(syscall.Handle(c.handle), &buffer, 1, &sent, 0, nil, nil)
		if err != nil {
			if retrySocketError(err) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return written, err
		}
		if sent == 0 {
			return written, io.ErrShortWrite
		}
		written += int(sent)
	}
	return written, nil
}

func retrySocketError(err error) bool {
	code, ok := err.(syscall.Errno)
	if !ok {
		return false
	}
	return code == 10035 || code == 997 || code == 10004
}

func resetSocketError(err error) bool {
	code, ok := err.(syscall.Errno)
	return ok && (code == 10052 || code == 10053 || code == 10054)
}

func openDoorSocket(handle uintptr) (stream, error) {
	if handle == 0 {
		return nil, fmt.Errorf("socket handle is required")
	}
	if err := winsock.Load(); err != nil {
		return nil, fmt.Errorf("load Winsock: %w", err)
	}
	var data [512]byte
	result, _, _ := winsockStartup.Call(uintptr(0x0202), uintptr(unsafe.Pointer(&data[0])))
	if result != 0 {
		return nil, fmt.Errorf("initialize Winsock: error %d", result)
	}
	socket := syscall.Handle(handle)
	if !socketConnected(socket) {
		duplicate, _, tried, ok := adoptSocket(socket)
		if !ok {
			parents := make([]string, 0, len(tried))
			for _, parent := range tried {
				parents = append(parents, fmt.Sprintf("%s(pid %d)", parent.name, parent.pid))
			}
			return nil, fmt.Errorf("door32 socket %d is not usable in this process and could not be duplicated from a parent (checked %s)", handle, parents)
		}
		socket = duplicate
	}
	var nonblocking uint32
	if result, _, callErr := winsockIoctl.Call(uintptr(socket), 0x8004667e, uintptr(unsafe.Pointer(&nonblocking))); result != 0 {
		if callErr == syscall.Errno(0) {
			callErr = syscall.EINVAL
		}
		return nil, fmt.Errorf("set blocking socket mode: %w", callErr)
	}
	setSocketTimeout(socket, 0x1006, 86400000)
	setSocketTimeout(socket, 0x1005, 86400000)
	return socketConn{handle: uintptr(socket)}, nil
}

func socketConnected(handle syscall.Handle) bool {
	var socketType int32
	length := int32(unsafe.Sizeof(socketType))
	if err := syscall.Getsockopt(handle, 0xffff, 0x1008, (*byte)(unsafe.Pointer(&socketType)), &length); err != nil {
		return false
	}
	_, err := syscall.Getpeername(handle)
	return err == nil || err == syscall.Errno(10057)
}

func setSocketTimeout(handle syscall.Handle, option int32, milliseconds int32) {
	winsockSetSock.Call(
		uintptr(handle),
		uintptr(0xffff),
		uintptr(option),
		uintptr(unsafe.Pointer(&milliseconds)),
		uintptr(unsafe.Sizeof(milliseconds)),
	)
}

func adoptSocket(handle syscall.Handle) (syscall.Handle, processInfo, []processInfo, bool) {
	const processDupHandle = 0x0040
	self, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, processInfo{}, nil, false
	}
	parents := processAncestors(6)
	for _, parent := range parents {
		process, err := syscall.OpenProcess(processDupHandle, false, parent.pid)
		if err != nil {
			continue
		}
		var duplicate syscall.Handle
		err = syscall.DuplicateHandle(process, handle, self, &duplicate, 0, false, 2)
		_ = syscall.CloseHandle(process)
		if err == nil {
			if socketConnected(duplicate) {
				return duplicate, parent, parents, true
			}
			winsockClose.Call(uintptr(duplicate))
		}
	}
	return 0, processInfo{}, parents, false
}

func processAncestors(depth int) []processInfo {
	snapshot, err := syscall.CreateToolhelp32Snapshot(0x00000002, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snapshot)
	parents := make(map[uint32]uint32)
	names := make(map[uint32]string)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := syscall.Process32First(snapshot, &entry); err == nil; err = syscall.Process32Next(snapshot, &entry) {
		parents[entry.ProcessID] = entry.ParentProcessID
		names[entry.ProcessID] = syscall.UTF16ToString(entry.ExeFile[:])
	}
	var result []processInfo
	seen := make(map[uint32]bool)
	pid := uint32(os.Getpid())
	for len(result) < depth {
		parent, ok := parents[pid]
		if !ok || parent == 0 || seen[parent] {
			break
		}
		seen[parent] = true
		result = append(result, processInfo{pid: parent, name: names[parent]})
		pid = parent
	}
	return result
}
