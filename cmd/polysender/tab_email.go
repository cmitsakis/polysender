package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

func tabEmail(w fyne.Window) *container.TabItem {
	subTabs := container.NewAppTabs(tabEmailIdentities(w), tabEmailSMTP(w), tabEmailSettings(w))
	return container.NewTabItemWithIcon("Email", theme.MailComposeIcon(), container.NewMax(subTabs))
}
