package adb

import (
	"fmt"
	"strings"

	"github.com/electricbubble/gadb"

	"go.polysender.org/internal/dbutil"
)

type Devices map[string]Device

func (devs Devices) ToSliceOfSaveables() []dbutil.Saveable {
	s := make([]dbutil.Saveable, 0, len(devs))
	for _, dev := range devs {
		s = append(s, dev)
	}
	return s
}

func GetDeviceWithAndroidID(androidID string) (Device, error) {
	devs := make(Devices)
	if err := GetDevices(devs); err != nil {
		return Device{}, fmt.Errorf("error while searching for ADB devices: %s", err)
	}
	dev, exists := devs[androidID]
	if !exists {
		return Device{}, fmt.Errorf("not found")
	}
	return dev, nil
}

func GetDevices(devs Devices) error {
	adbClient, err := gadb.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create ADB client: %w", err)
	}
	adbDeviceList, err := adbClient.DeviceList()
	if err != nil {
		return fmt.Errorf("DeviceList failed: %w", err)
	}
	reachableIDs := make(map[string]struct{}, len(adbDeviceList))
	for _, adbDevice := range adbDeviceList {
		// get Android ID
		aID, err := adbDevice.RunShellCommand("settings", "get", "secure", "android_id")
		if err != nil {
			return fmt.Errorf("runAdbCommand failed: %w", err)
		}
		aID = strings.TrimSpace(aID)
		if aID == "" {
			return fmt.Errorf("shell command returned empty android_id. error: %s", err)
		}
		reachableIDs[aID] = struct{}{}
		// stop if it already knows that device
		if _, ok := devs[aID]; ok {
			continue
		}
		// get hostname
		hostname, err := adbDevice.RunShellCommand("getprop", "net.hostname")
		if err != nil {
			return fmt.Errorf("runAdbCommand failed: %w", err)
		}
		hostname = strings.TrimSpace(hostname)
		devs[aID] = Device{adbDevice: adbDevice, AndroidID: aID, Name: hostname, serial: adbDevice.Serial(), reachable: true}
	}
	devs.markUnreachable(reachableIDs)

	return nil
}

func (devs Devices) markUnreachable(reachableIDs map[string]struct{}) {
	for aID, dev := range devs {
		if _, exists := reachableIDs[aID]; !exists {
			// TODO: lock before editing. or substitute it with a different device pointer
			dev.reachable = false
			devs[aID] = dev
		}
	}
}
