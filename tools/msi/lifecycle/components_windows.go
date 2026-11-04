package main

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// componentObservation retains actual MSI state and keypath results without repair.
type componentObservation struct {
	// Product is the queried ProductCode.
	Product string `json:"product"`
	// Component is the queried component GUID.
	Component string `json:"component"`
	// State is the native INSTALLSTATE result, including non-installed/error states.
	State int32 `json:"installstate"`
	// KeyPath is the returned native component keypath, if any.
	KeyPath string `json:"keypath"`
}

func readFeatureAndKeypaths(u userContext, all []media) (map[string]any, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result := map[string]any{}
	sid, err := windows.UTF16PtrFromString(u.SID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, m := range all {
		if seen[m.Product] {
			continue
		}
		seen[m.Product] = true
		product, err := windows.UTF16PtrFromString(m.Product)
		if err != nil {
			return nil, err
		}
		feature, err := windows.UTF16PtrFromString("Bridge")
		if err != nil {
			return nil, err
		}
		var state int32
		status, _, _ := msi.NewProc("MsiQueryFeatureStateExW").Call(uintptr(unsafe.Pointer(product)),
			uintptr(unsafe.Pointer(sid)), 2, uintptr(unsafe.Pointer(feature)), uintptr(unsafe.Pointer(&state)))
		record := map[string]any{"feature_msi_code": uint32(status), "feature_state": state}
		var components []componentObservation
		for _, guid := range m.Components {
			component, err := windows.UTF16PtrFromString(guid)
			if err != nil {
				return nil, err
			}
			var path [1024]uint16
			size := uint32(len(path))
			value, _, _ := msi.NewProc("MsiGetComponentPathExW").Call(uintptr(unsafe.Pointer(product)),
				uintptr(unsafe.Pointer(component)), uintptr(unsafe.Pointer(sid)), 2,
				uintptr(unsafe.Pointer(&path[0])), uintptr(unsafe.Pointer(&size)))
			if int32(value) == -3 {
				return result, errors.New("oversized component diagnostic keypath")
			}
			components = append(components, componentObservation{m.Product, guid, int32(value), windows.UTF16ToString(path[:])})
		}
		record["component_keypaths"] = components
		result[m.Product] = record
	}
	return result, nil
}
