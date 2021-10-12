package android

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/gateway"
	"go.polysender.org/internal/gateway/sms/android/adb"
	"go.polysender.org/internal/gateway/sms/android/kde"
)

type Device struct {
	AndroidID      string
	Name           string
	adb            *adb.Device
	kde            *kde.Device
	limitPerMinute SettingLimitPerMinute
	limitPerHour   SettingLimitPerHour
	limitPerDay    SettingLimitPerDay
}

var (
	_ gateway.Gateway      = (*Device)(nil)
	_ gateway.SenderClient = (*Device)(nil)
)

func (d Device) DBTable() string {
	return "gateway.sms.android.device"
}

func (d Device) DBKey() []byte {
	return []byte(d.AndroidID)
}

func (d Device) GetLimitPerMinute() int {
	return int(d.limitPerMinute)
}

func (d Device) GetLimitPerHour() int {
	return int(d.limitPerHour)
}

func (d Device) GetLimitPerDay() int {
	return int(d.limitPerDay)
}

func (d Device) GetConcurrencyMax() int {
	return 1
}

func (d Device) String() string {
	return fmt.Sprintf("androidID: %v, name: %v", d.AndroidID, d.Name)
}

func NewDeviceFromKey(tx *bolt.Tx, key []byte) (*Device, error) {
	var dev Device
	err := dbutil.GetByKeyTx(tx, key, &dev)
	if err != nil { // don't ignore dbutil.ErrNotFound
		return nil, fmt.Errorf("failed to read device from database: %s", err)
	}
	err = dbutil.GetByKeyTx(tx, dev.limitPerMinute.DBKey(), &dev.limitPerMinute)
	if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
		return nil, fmt.Errorf("failed to read device from database: %s", err)
	}
	err = dbutil.GetByKeyTx(tx, dev.limitPerHour.DBKey(), &dev.limitPerHour)
	if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
		return nil, fmt.Errorf("failed to read device from database: %s", err)
	}
	err = dbutil.GetByKeyTx(tx, dev.limitPerDay.DBKey(), &dev.limitPerDay)
	if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
		return nil, fmt.Errorf("failed to read device from database: %s", err)
	}
	return &dev, nil
}

type deviceAble interface {
	DeviceAndroidID() string
	DeviceName() string
}

func FromDeviceable(d deviceAble) Device {
	return Device{
		AndroidID: d.DeviceAndroidID(),
		Name:      d.DeviceName(),
	}
}

var ErrDeviceUnreachable = errors.New("device unreachable")

func (d *Device) PreSend(ctx context.Context) error {
	devAdb, errAdb := adb.GetDeviceWithAndroidID(d.AndroidID)
	devKde, errKde := kde.GetDeviceWithAndroidID(ctx, d.AndroidID)
	if errAdb != nil && errKde != nil {
		return fmt.Errorf("failed to find connected device via ADB (error: %s) and KDE Connect (error: %s): %w", errAdb, errKde, ErrDeviceUnreachable)
	}
	var reachable bool
	if errAdb == nil {
		d.adb = &devAdb
		err := d.adb.PreSend()
		if err != nil {
			d.adb = nil
		}
		if err == nil && devAdb.Reachable() {
			reachable = true
		}
	}
	if errKde == nil {
		d.kde = devKde
		if devKde.Reachable {
			reachable = true
		}
	}
	if !reachable {
		return ErrDeviceUnreachable
	}
	return nil
}

func (d Device) PostSend(ctx context.Context) error {
	err := d.kde.Conn.Close()
	if err != nil {
		return fmt.Errorf("failed to close kde connection to device: %w", err)
	}
	return nil
}

func (d Device) Send(ctx context.Context, to string, subject, msg, broadcastID string) error {
	msg = strings.TrimSpace(msg)
	if d.adb != nil {
		err := d.adb.SendSMS(to, msg)
		if err != nil {
			return fmt.Errorf("failed to send SMS via ADB: %s", err)
		}
	} else if d.kde != nil && d.kde.Reachable {
		err := d.kde.SendSMS(to, msg)
		if err != nil {
			return fmt.Errorf("failed to send SMS via KDE Connect: %s", err)
		}
	} else {
		return fmt.Errorf("device is not connected")
	}
	return nil
}
