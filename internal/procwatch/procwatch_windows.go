package procwatch

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Running returns the names of the folders under root that contain the
// executable of at least one running process.
func Running(root string) (map[string]bool, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)

	running := map[string]bool{}
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		exe, ok := imagePath(entry.ProcessID)
		if !ok {
			continue
		}
		if folder := folderUnder(root, exe); folder != "" {
			running[folder] = true
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return running, nil
}

func imagePath(pid uint32) (string, bool) {
	// Elevated processes refuse this from a normal user; those are skipped.
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}
