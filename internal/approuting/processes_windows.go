package approuting

import (
	"errors"
	"golang.org/x/sys/windows"
	"sort"
	"strings"
	"unsafe"
)

type RunningApplication struct {
	Path string   `json:"path"`
	ID   string   `json:"id"`
	PIDs []uint32 `json:"pids"`
}

// PIDs are display-only. Selection always uses the resolved executable AppID.
// Inaccessible/exited processes are counted, never guessed from their basename.
func RunningApplications() ([]RunningApplication, int, error) {
	snap, e := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return nil, 0, ErrExecutable
	}
	defer windows.CloseHandle(snap)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	e = windows.Process32First(snap, &entry)
	grouped := map[string]*RunningApplication{}
	cache := map[string]Identity{}
	skipped := 0
	for e == nil {
		pid := entry.ProcessID
		if pid != 0 {
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
			if err != nil {
				skipped++
			} else {
				buf := make([]uint16, 32768)
				n := uint32(len(buf))
				err = windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
				windows.CloseHandle(h)
				if err != nil {
					skipped++
				} else {
					path := windows.UTF16ToString(buf[:n])
					key := strings.ToLower(path)
					id, ok := cache[key]
					if !ok {
						id, err = ResolveExecutable(path)
						if err == nil {
							cache[key] = id
						}
					}
					if err != nil {
						skipped++
					} else {
						app := grouped[id.ID]
						if app == nil {
							app = &RunningApplication{Path: id.Path, ID: id.ID}
							grouped[id.ID] = app
						}
						app.PIDs = append(app.PIDs, pid)
					}
				}
			}
		}
		e = windows.Process32Next(snap, &entry)
	}
	if !errors.Is(e, windows.ERROR_NO_MORE_FILES) {
		return nil, skipped, ErrExecutable
	}
	apps := make([]RunningApplication, 0, len(grouped))
	for _, app := range grouped {
		sort.Slice(app.PIDs, func(i, j int) bool { return app.PIDs[i] < app.PIDs[j] })
		apps = append(apps, *app)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Path < apps[j].Path })
	return apps, skipped, nil
}
