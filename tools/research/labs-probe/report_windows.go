package main

import (
	"golang.org/x/sys/windows"
)

func publishReportFile(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	// No REPLACE_EXISTING or cross-volume copy: publication must not overwrite a record.
	return windows.MoveFileEx(source, destination, 0)
}
