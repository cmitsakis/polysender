package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

func tabBroadcasts(w fyne.Window) *container.TabItem {
	subTabs := container.NewAppTabs(tabBroadcastsSendQueue(w), tabBroadcastsSettings(w))
	return container.NewTabItemWithIcon("Broadcasts", theme.MailSendIcon(), container.NewMax(subTabs))
}
