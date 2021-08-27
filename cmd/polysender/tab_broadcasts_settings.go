package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"go.polysender.org/internal/broadcast"
	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/fyneutil/form"
	"go.polysender.org/internal/tzdb"
)

func tabBroadcastsSettings(w fyne.Window) *container.TabItem {
	refreshChan := make(chan struct{}, 1)

	sendHoursValue := form.NewValue(w, "", func(labelUpdates chan<- string) {
		var existingSendHours broadcast.SettingSendHours
		err := dbutil.GetByKey(db, existingSendHours.DBKey(), &existingSendHours)
		if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		form.ShowEntryPopup(w, "Send hours", "Time ranges in 24 hour format (e.g. 13-14 15-17)", "e.g. 9-13 14-17", existingSendHours.String(), func(inputText string) error {
			sendHours, err := broadcast.ParseTimeRanges(inputText)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid time ranges: %s", err))
			}
			err = dbutil.UpsertSaveable(db, broadcast.SettingSendHours(sendHours))
			if err != nil {
				return logAndReturnError(fmt.Errorf("database error: %s", err))
			}
			// refreshChan <- struct{}{}
			labelUpdates <- broadcast.SettingSendHours(sendHours).String()
			return nil
		})
	}, func(labelUpdates chan<- string) {
		err := dbutil.UpsertSaveable(db, broadcast.SettingSendHours([]broadcast.TimeRange{}))
		if err != nil {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		// refreshChan <- struct{}{}
		labelUpdates <- ""
	})

	timezoneValue := form.NewValue(w, "If not set, computer time is used", func(labelUpdates chan<- string) {
		form.ShowEntryCompletionPopup(w, "Time zone", "Search by country or time zone and select a result", "", "", tzdb.TimeZones, form.FilterOptions, func(inputText string) error {
			fields := strings.Fields(inputText)
			if len(fields) == 0 {
				return logAndReturnError(fmt.Errorf("invalid time zone"))
			}
			tzName := fields[0]
			_, err := time.LoadLocation(tzName)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid time zone: %s", err))
			}
			err = dbutil.UpsertSaveable(db, broadcast.SettingTimezone(tzName))
			if err != nil {
				return logAndReturnError(fmt.Errorf("database error: %s", err))
			}
			// refreshChan <- struct{}{}
			labelUpdates <- tzName
			return nil
		})
	}, func(labelUpdates chan<- string) {
		err := dbutil.UpsertSaveable(db, broadcast.SettingTimezone(""))
		if err != nil {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		// refreshChan <- struct{}{}
		labelUpdates <- ""
	})

	f := &widget.Form{}
	f.Append("Send hours:", sendHoursValue)
	f.Append("Time zone:", timezoneValue)

	go func() {
		for range refreshChan {
			var settingSendHours broadcast.SettingSendHours
			var settingTimezone broadcast.SettingTimezone
			err := dbutil.GetMulti(
				db,
				dbutil.KeyPointer{Key: settingSendHours.DBKey(), Pointer: &settingSendHours},
				dbutil.KeyPointer{Key: settingTimezone.DBKey(), Pointer: &settingTimezone},
			)
			if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
				loggerDebug.Println("dbutil.GetMulti failed:", err)
			}
			sendHoursValue.Objects[0].(*widget.Label).SetText(fmt.Sprintf("%v", settingSendHours))
			timezoneValue.Objects[0].(*widget.Label).SetText(fmt.Sprintf("%v", settingTimezone))
		}
	}()

	refreshChan <- struct{}{}

	return container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), container.NewScroll(f))
}
