package kde

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	"github.com/godbus/dbus/v5"
)

type Device struct {
	AndroidID string
	Name      string
	Type      string
	Reachable bool
	Trusted   bool
	Plugins   map[string]struct{}
	Conn      *dbus.Conn
}

func GetDeviceWithAndroidID(ctx context.Context, androidID string) (*Device, error) {
	devs := make(Devices)
	_, err := GetDevices(ctx, nil, devs)
	if err != nil {
		return nil, fmt.Errorf("error while searching for KDE Connect devices: %s", err)
	}
	dev, exists := devs[androidID]
	if !exists {
		return nil, fmt.Errorf("not found")
	}
	return dev, nil
}

func (d Device) PermissionSMS() bool {
	if _, exists := d.Plugins["kdeconnect_sms"]; !exists {
		return false
	}
	return true
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

func (d *Device) dbusGetPropsAndSet() error {
	for i := 1; i < len(propNames); i++ {
		if err := d.dbusGetPropAndSet(i); err != nil {
			return fmt.Errorf("failed to get or set property: %s", err)
		}
	}
	return nil
}

func (d *Device) dbusGetPropAndSet(signalIndex int) error {
	propName := propNames[signalIndex]
	propVal, err := d.Conn.Object(serviceName, dbus.ObjectPath(servicePath+"/devices/"+d.AndroidID)).GetProperty(serviceName + ".device." + propName)
	if err != nil {
		return fmt.Errorf("d.Conn.Object() failed: %w", err)
	}
	dReflectElem := reflect.ValueOf(d).Elem()
	field := dReflectElem.FieldByName(fieldNames[signalIndex])
	switch fieldTypes[signalIndex] {
	case "bool":
		field.SetBool(propVal.Value().(bool))
	case "string":
		propUnquoted, err := strconv.Unquote(propVal.String())
		if err != nil {
			return fmt.Errorf("strconv.Unquote failed: %s", err)
		}
		field.SetString(propUnquoted)
	case "set":
		set := make(map[string]struct{})
		for _, element := range propVal.Value().([]string) {
			set[element] = struct{}{}
		}
		field.Set(reflect.ValueOf(set))
	}
	return nil
}

func (d *Device) SendSMS(to string, msg string) error {
	if _, exists := d.Plugins["kdeconnect_sms"]; !exists {
		return fmt.Errorf("SMS plugin not enabled")
	}
	return d.Conn.Object(serviceName, dbus.ObjectPath(servicePath+"/devices/"+d.AndroidID+"/sms")).Call(serviceName+".device.sms.sendSms", 0, []interface{}{to}, msg, []interface{}{}).Err
}
