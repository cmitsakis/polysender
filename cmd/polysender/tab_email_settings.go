package main

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/fyneutil/form"
	"go.polysender.org/internal/gateway/email"
)

func tabEmailSettings(w fyne.Window) *container.TabItem {
	refreshChan := make(chan struct{}, 1)

	listUnsubscribeEnabledCheck := widget.NewCheck("", nil)

	updateCheck := func() {
		var existingListUnsubscribeEnabled email.SettingListUnsubscribeEnabled
		err := dbutil.GetByKey(db, existingListUnsubscribeEnabled.DBKey(), &existingListUnsubscribeEnabled)
		if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
		} else {
			listUnsubscribeEnabledCheck.SetChecked(bool(existingListUnsubscribeEnabled))
		}
	}

	updateCheck()

	listUnsubscribeEnabledCheck.OnChanged = func(value bool) {
		loggerDebug.Printf("[ListUnsubscribeEnabled] OnChanged value=%v\n", value)
		err := dbutil.UpsertSaveable(db, email.SettingListUnsubscribeEnabled(value))
		if err != nil {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
		}
		updateCheck()
	}

	listUnsubscribeEmailValue := form.NewValue(w, "If you want to use one email for all unsubscribe requests. Leave empty to use sending email.", func(labelUpdates chan<- string) {
		var existingListUnsubscribeEmailIdentity email.Identity
		var emailIdentities []string
		if err := db.View(func(tx *bolt.Tx) error {
			err := dbutil.ForEachTx(tx, &email.Identity{}, func(k []byte, v interface{}) error {
				emailIdentities = append(emailIdentities, string(v.(email.Identity).DBKey()))
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to read email identity from database: %s", err)
			}
			return email.DBGetSettingListUnsubscribeEmailIdentity(tx, &existingListUnsubscribeEmailIdentity)
		}); err != nil {
			logAndShowError(fmt.Errorf("database error: %s", err), w)
			return
		}
		form.ShowSelectionPopup(w, "List-Unsubscribe email", "Select email", "Save", emailIdentities, existingListUnsubscribeEmailIdentity.Email, func(selected string, selectedIndex int) error {
			loggerDebug.Println("selected", selected)
			if selected != "" {
				err := dbutil.UpsertSaveable(db, email.SettingListUnsubscribeEmailKey(selected))
				if err != nil {
					return logAndReturnError(fmt.Errorf("failed to update record on database: %s", err))
				}
				// refreshChan <- struct{}{}
				labelUpdates <- selected
			}
			return nil
		})
	}, func(labelUpdates chan<- string) {
		err := dbutil.DeleteByTableKey(db, email.SettingListUnsubscribeEmailKey("").DBTable(), email.SettingListUnsubscribeEmailKey("").DBKey())
		if err != nil {
			logAndShowError(fmt.Errorf("failed to delete record from database: %s", err), w)
			return
		}
		// refreshChan <- struct{}{}
		labelUpdates <- ""
	})

	f := &widget.Form{}
	f.Append("WARNING:", widget.NewLabel("You have to handle unsubscribe emails manually by removing emails from your lists."))
	f.Append("Enable List-Unsubscribe:", listUnsubscribeEnabledCheck)
	f.Append("List-Unsubscribe email:", listUnsubscribeEmailValue)

	go func() {
		for range refreshChan {
			var settingListUnsubscribeEmailIdentity email.Identity
			if err := db.View(func(tx *bolt.Tx) error {
				return email.DBGetSettingListUnsubscribeEmailIdentity(tx, &settingListUnsubscribeEmailIdentity)
			}); err != nil {
				logAndShowError(fmt.Errorf("failed to read settings from database: %s", err), w)
				return
			}
			listUnsubscribeEmailValue.Objects[0].(*widget.Label).SetText(settingListUnsubscribeEmailIdentity.Email)
		}
	}()

	refreshChan <- struct{}{}
	content := widget.NewCard("Unsubscribe requests", "", f)
	return container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), container.NewScroll(content))
}
