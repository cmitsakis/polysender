package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"go.polysender.org/internal/dbutil"
	container2 "go.polysender.org/internal/fyneutil/container"
	widget2 "go.polysender.org/internal/fyneutil/widget"
	"go.polysender.org/internal/gateway/sms/android"
	"go.polysender.org/internal/gateway/sms/android/adb"
)

func tabSmsAndroidAdb(w fyne.Window) *container.TabItem {
	refreshChan := make(chan struct{}, 1)
	tablePage := container2.NewTable(
		w,
		refreshChan,
		[]widget2.TableAttribute{
			{Name: "Actions", Actions: true},
			{Name: "Android ID", Field: "AndroidID", Width: 175},
			{Name: "Serial", Field: "Serial", Width: 175},
			{Name: "Name", Field: "Name", Width: 175},
			{Name: "Reachable", Field: "Reachable", Width: 125},
		},
		[]widget2.Action{
			{
				Name: "Save",
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						d := v.(adb.Device)
						err := dbutil.UpsertSaveable(db, android.FromDeviceable(d))
						if err != nil {
							logAndShowError(fmt.Errorf("database error: %s", err), w)
						}
					}
				},
			},
		},
		func(refreshChan <-chan struct{}, t *widget2.Table, noticeLabel *widget.Label) {
			devs := make(adb.Devices)
			for range refreshChan {
				err := adb.GetDevices(devs)
				if err != nil {
					err = fmt.Errorf("adb.GetDevices() failed: %s", err)
					loggerInfo.Println(err.Error())
					noticeLabel.SetText(err.Error())
				} else {
					noticeLabel.SetText("")
				}
				t.UpdateAndRefresh(devs.ToSliceOfSaveables())
			}
		},
	)
	refreshChan <- struct{}{}
	return container.NewTabItemWithIcon("Connected Devices (ADB)", theme.ComputerIcon(), tablePage)
}
