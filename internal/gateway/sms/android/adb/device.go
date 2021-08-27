package adb

import (
	"github.com/electricbubble/gadb"
)

type Device struct {
	AndroidID           string
	Name                string
	adbDevice           gadb.Device
	reachable           bool
	serial              string
	androidVersionMajor int
	serviceDomain       string
}

func (d Device) DBTable() string {
	return "gateway.sms.android.device"
}

func (d Device) DBKey() []byte {
	return []byte(d.AndroidID)
}

func (d Device) DeviceAndroidID() string {
	return d.AndroidID
}

func (d Device) DeviceName() string {
	return d.Name
}

func (d Device) Reachable() bool {
	return d.reachable
}

func (d Device) Serial() string {
	return d.adbDevice.Serial()
}
