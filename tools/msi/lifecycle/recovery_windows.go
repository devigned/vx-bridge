package main

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func inspectPending() (*pendingEvidence, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, installerKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data := make([]byte, pendingSize)
	n, kind, readErr := key.GetValue("Pending", data)
	closeErr := key.Close()
	if errors.Is(readErr, registry.ErrNotExist) {
		return nil, closeErr
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if n != pendingSize || kind != registry.BINARY {
		return nil, errors.New("actual pending size/type differs")
	}
	return decodePending(data)
}

func componentClients(u userContext, all []media) (map[string][]string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	present := map[string]bool{}
	for index := uint32(0); index < 256; index++ {
		var guid [39]uint16
		var sid [256]uint16
		var context uint32
		size := uint32(len(sid))
		// NULL selects this already-proven primary-token user; outputs must still
		// identify that exact SID and unmanaged context.
		status, _, _ := enumComponents.Call(0, 2, uintptr(index), uintptr(unsafe.Pointer(&guid[0])),
			uintptr(unsafe.Pointer(&context)), uintptr(unsafe.Pointer(&sid[0])), uintptr(unsafe.Pointer(&size)))
		if status == 259 {
			break
		}
		if status != 0 || context != 2 || windows.UTF16ToString(sid[:]) != u.SID {
			return nil, fmt.Errorf("MsiEnumComponentsEx: code=%d context=%d", status, context)
		}
		present[windows.UTF16ToString(guid[:])] = true
		if index == 255 {
			return nil, errors.New("oversized component inventory")
		}
	}
	result := map[string][]string{}
	for _, m := range all {
		for _, guid := range m.Components {
			if _, exists := result[guid]; exists {
				continue
			}
			component, err := windows.UTF16PtrFromString(guid)
			if err != nil {
				return nil, err
			}
			result[guid] = []string{}
			if !present[guid] {
				continue
			}
			for index := uint32(0); index < 128; index++ {
				var code [39]uint16
				var sid [256]uint16
				var context uint32
				size := uint32(len(sid))
				status, _, _ := enumClients.Call(uintptr(unsafe.Pointer(component)), 0, 2, uintptr(index),
					uintptr(unsafe.Pointer(&code[0])), uintptr(unsafe.Pointer(&context)), uintptr(unsafe.Pointer(&sid[0])), uintptr(unsafe.Pointer(&size)))
				if status == 259 {
					break
				}
				if status != 0 || context != 2 || windows.UTF16ToString(sid[:]) != u.SID {
					return result, fmt.Errorf("MsiEnumClientsEx: code=%d context=%d", status, context)
				}

				result[guid] = append(result[guid], windows.UTF16ToString(code[:]))
				if index == 127 {
					return result, errors.New("unexpected oversized component client inventory")
				}
			}
		}
	}
	return result, nil
}

func verifyProductAbsent(m media, u userContext) error {
	product, err := inspectProduct(m.Product, u)
	if err != nil || product.Registered {
		return fmt.Errorf("owned product/advertised registration remains: %+v: %v", product, err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Uninstall\`+m.Product, registry.READ|registry.WOW64_64KEY)
	if err == nil {
		return errors.Join(errors.New("owned standard ARP key remains"), key.Close())
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
