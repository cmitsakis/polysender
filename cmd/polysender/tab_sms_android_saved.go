package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"go.polysender.org/internal/dbutil"
	container2 "go.polysender.org/internal/fyneutil/container"
	widget2 "go.polysender.org/internal/fyneutil/widget"
	"go.polysender.org/internal/gateway/sms/android"
)

func tabSmsAndroidSaved(w fyne.Window) *container.TabItem {
	refreshChan := make(chan struct{}, 1)
	tablePage := container2.NewTable(
		w,
		refreshChan,
		[]widget2.TableAttribute{
			{Name: "Actions", Actions: true},
			{Name: "Android ID", Field: "AndroidID", Width: 175},
			{Name: "Name", Field: "Name", Width: 175},
		},
		[]widget2.Action{
			{
				Name: "Delete",
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						content := widget.NewLabel("Are you sure you want to delete this device?")
						dialog.ShowCustomConfirm("Delete Device", "Confirm", "Cancel", content, func(submit bool) {
							if submit {
								err := dbutil.DeleteByTableKey(db, v.DBTable(), v.DBKey())
								if err != nil {
									logAndShowError(fmt.Errorf("failed to delete device from database: %s", err), w)
								}
								refreshChan <- struct{}{}
							}
						}, w)
					}
				},
			},
		},
		func(refreshChan <-chan struct{}, t *widget2.Table, noticeLabel *widget.Label) {
			for range refreshChan {
				values := make([]dbutil.Saveable, 0)
				err := dbutil.ForEach(db, &android.Device{}, func(k []byte, v interface{}) error {
					vCasted, ok := v.(android.Device)
					if !ok {
						return fmt.Errorf("v is not a device")
					}
					values = append(values, vCasted)
					return nil
				})
				if err != nil {
					err = fmt.Errorf("cannot read broadcast: %s", err)
					loggerInfo.Println(err)
					noticeLabel.SetText(err.Error())
				} else {
					noticeLabel.SetText("")
				}
				t.UpdateAndRefresh(values)
			}
		},
	)
	refreshChan <- struct{}{}
	return container.NewTabItemWithIcon("Saved Devices", theme.ComputerIcon(), tablePage)
}
