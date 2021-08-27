package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

func tabSMSAndroid(w fyne.Window) *container.TabItem {
	subTabs := container.NewAppTabs(tabSmsAndroidSaved(w), tabSmsAndroidAdb(w), tabSmsAndroidKde(w), tabSmsAndroidSettings(w))
	return container.NewTabItemWithIcon("SMS (Android)", theme.ComputerIcon(), container.NewMax(subTabs))
}
