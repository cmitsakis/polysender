package main

import (
	"errors"
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/fyneutil/form"
	"go.polysender.org/internal/gateway/sms/android"
)

func tabSmsAndroidSettings(w fyne.Window) *container.TabItem {
	refreshChan := make(chan struct{}, 1)

	limitPerMinValue := form.NewValue(w, "", func(labelUpdates chan<- string) {
		var settingLimitPerMinute android.SettingLimitPerMinute
		err := dbutil.GetByKey(db, settingLimitPerMinute.DBKey(), &settingLimitPerMinute)
		if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		form.ShowEntryPopup(w, "Limit per minute", "Maximum number of messages per minute per device (0 = no limit)", "", fmt.Sprintf("%v", settingLimitPerMinute), func(inputText string) error {
			inputUint, err := strconv.ParseUint(inputText, 10, 32)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid value: %s", err))
			}
			err = dbutil.UpsertSaveable(db, android.SettingLimitPerMinute(inputUint))
			if err != nil {
				return logAndReturnError(fmt.Errorf("database error: %s", err))
			}
			// refreshChan <- struct{}{}
			labelUpdates <- strconv.FormatUint(inputUint, 10)
			return nil
		})
	}, func(labelUpdates chan<- string) {
		err := dbutil.UpsertSaveable(db, android.SettingLimitPerMinute(0))
		if err != nil {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		// refreshChan <- struct{}{}
		labelUpdates <- strconv.FormatUint(0, 10)
	})

	limitPerHourValue := form.NewValue(w, "", func(labelUpdates chan<- string) {
		var settingLimitPerHour android.SettingLimitPerHour
		err := dbutil.GetByKey(db, settingLimitPerHour.DBKey(), &settingLimitPerHour)
		if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		form.ShowEntryPopup(w, "Limit per hour", "Maximum number of messages per hour per device (0 = no limit)", "", fmt.Sprintf("%v", settingLimitPerHour), func(inputText string) error {
			inputUint, err := strconv.ParseUint(inputText, 10, 32)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid value: %s", err))
			}
			err = dbutil.UpsertSaveable(db, android.SettingLimitPerHour(inputUint))
			if err != nil {
				return logAndReturnError(fmt.Errorf("database error: %s", err))
			}
			// refreshChan <- struct{}{}
			labelUpdates <- strconv.FormatUint(inputUint, 10)
			return nil
		})
	}, func(labelUpdates chan<- string) {
		err := dbutil.UpsertSaveable(db, android.SettingLimitPerHour(0))
		if err != nil {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		// refreshChan <- struct{}{}
		labelUpdates <- strconv.FormatUint(0, 10)
	})

	limitPerDayValue := form.NewValue(w, "", func(labelUpdates chan<- string) {
		var settingLimitPerDay android.SettingLimitPerDay
		err := dbutil.GetByKey(db, settingLimitPerDay.DBKey(), &settingLimitPerDay)
		if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		form.ShowEntryPopup(w, "Limit per day", "Maximum number of messages per day per device (0 = no limit)", "", fmt.Sprintf("%v", settingLimitPerDay), func(inputText string) error {
			inputUint, err := strconv.ParseUint(inputText, 10, 32)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid value: %s", err))
			}
			err = dbutil.UpsertSaveable(db, android.SettingLimitPerDay(inputUint))
			if err != nil {
				return logAndReturnError(fmt.Errorf("database error: %s", err))
			}
			// refreshChan <- struct{}{}
			labelUpdates <- strconv.FormatUint(inputUint, 10)
			return nil
		})
	}, func(labelUpdates chan<- string) {
		err := dbutil.UpsertSaveable(db, android.SettingLimitPerDay(0))
		if err != nil {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		// refreshChan <- struct{}{}
		labelUpdates <- strconv.FormatUint(0, 10)
	})

	go func() {
		for range refreshChan {
			var settingLimitPerMinute android.SettingLimitPerMinute
			var settingLimitPerHour android.SettingLimitPerHour
			var settingLimitPerDay android.SettingLimitPerDay
			err := dbutil.GetMulti(
				db,
				dbutil.KeyPointer{Key: settingLimitPerMinute.DBKey(), Pointer: &settingLimitPerMinute},
				dbutil.KeyPointer{Key: settingLimitPerHour.DBKey(), Pointer: &settingLimitPerHour},
				dbutil.KeyPointer{Key: settingLimitPerDay.DBKey(), Pointer: &settingLimitPerDay},
			)
			if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
				loggerDebug.Println("dbutil.GetMulti failed:", err)
			}
			limitPerMinValue.Objects[0].(*widget.Label).SetText(strconv.FormatUint(uint64(settingLimitPerMinute), 10))
			limitPerHourValue.Objects[0].(*widget.Label).SetText(strconv.FormatUint(uint64(settingLimitPerHour), 10))
			limitPerDayValue.Objects[0].(*widget.Label).SetText(strconv.FormatUint(uint64(settingLimitPerDay), 10))
		}
	}()

	refreshChan <- struct{}{}

	f := &widget.Form{}
	f.Append("per minute:", limitPerMinValue)
	f.Append("per hour:", limitPerHourValue)
	f.Append("per day:", limitPerDayValue)

	content := widget.NewCard("SMS sending limits (per device)", "", f)
	return container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), container.NewScroll(content))
}
