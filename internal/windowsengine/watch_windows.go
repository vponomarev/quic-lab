//go:build windows

package windowsengine

import (
	"context"
	"golang.org/x/sys/windows"
	"sync"
	"syscall"
	"unsafe"
)

var iphelper = windows.NewLazySystemDLL("iphlpapi.dll")
var notifyMu sync.Mutex
var notifyNext uintptr
var notifyListeners = map[uintptr]chan struct{}{}
var notifyCallback = syscall.NewCallback(func(id, row, kind uintptr) uintptr {
	notifyMu.Lock()
	ch := notifyListeners[id]
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	notifyMu.Unlock()
	return 0
})

func WatchWindowsIPv4(ctx context.Context) (<-chan struct{}, func(), error) {
	ch := make(chan struct{}, 1)
	notifyMu.Lock()
	notifyNext++
	id := notifyNext
	notifyListeners[id] = ch
	notifyMu.Unlock()
	var handles []uintptr
	closeWatch := func() {
		for _, h := range handles {
			iphelper.NewProc("CancelMibChangeNotify2").Call(h)
		}
		notifyMu.Lock()
		delete(notifyListeners, id)
		notifyMu.Unlock()
	}
	for _, name := range []string{"NotifyRouteChange2", "NotifyIpInterfaceChange", "NotifyUnicastIpAddressChange"} {
		var h uintptr
		r, _, _ := iphelper.NewProc(name).Call(windows.AF_INET, notifyCallback, id, 0, uintptr(unsafe.Pointer(&h)))
		if r != 0 {
			closeWatch()
			return nil, func() {}, syscall.Errno(r)
		}
		handles = append(handles, h)
	}
	return ch, closeWatch, nil
}
