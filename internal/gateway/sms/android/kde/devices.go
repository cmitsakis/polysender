package kde

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"

	"go.polysender.org/internal/dbutil"
)

type Devices map[string]*Device

func (devs Devices) ToSliceOfSaveables() []dbutil.Saveable {
	s := make([]dbutil.Saveable, 0, len(devs))
	for _, dev := range devs {
		devCopy := *dev
		s = append(s, &devCopy)
	}
	return s
}

func GetDevices(ctx context.Context, refreshChan chan<- struct{}, devs Devices) (*dbus.Conn, error) {
	var androidIDs []string
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("dbus.SessionBus() failed: %s", err)
	}
	if err := conn.Object(serviceName, servicePath).Call("devices", 0, false, true).Store(&androidIDs); err != nil {
		return conn, fmt.Errorf("dbus call 'devices' failed: %s", err)
	}
	if refreshChan != nil {
		err = dbusRegisterSignals(ctx, conn, refreshChan)
		if err != nil {
			return conn, fmt.Errorf("dev.RegisterSignals() failed: %s", err)
		}
	}
	for _, androidID := range androidIDs {
		dev := &Device{
			AndroidID: androidID,
			Conn:      conn,
		}
		err = dev.dbusGetPropsAndSet()
		if err != nil {
			return conn, fmt.Errorf("dev.dbusGetPropsAndSet() failed: %s", err)
		}
		devs[androidID] = dev
	}
	return conn, nil
}

func dbusRegisterSignals(ctx context.Context, conn *dbus.Conn, refreshChan chan<- struct{}) error {
	for _, signalName := range signalNames[:] {
		if signalName == "" {
			continue
		}
		err := conn.AddMatchSignalContext(
			ctx,
			dbus.WithMatchMember(signalName),
			dbus.WithMatchInterface(serviceName+".device"),
			dbus.WithMatchPathNamespace(servicePath),
		)
		if err != nil {
			return fmt.Errorf("DBus.AddMatch failed for %s: %w", signalName, err)
		}
	}

	signalChan := make(chan *dbus.Signal, 1)
	conn.Signal(signalChan)
	go func(signalChan chan *dbus.Signal) {
		for {
			select {
			case <-ctx.Done():
				goto exitFor
			case <-signalChan:
				if refreshChan != nil {
					select {
					case refreshChan <- struct{}{}:
					default:
					}
				}
			}
		}
	exitFor:
		conn.RemoveSignal(signalChan)
	}(signalChan)

	return nil
}
