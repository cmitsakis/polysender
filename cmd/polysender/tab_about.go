package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

func tabAbout(w fyne.Window) *container.TabItem {
	subTabs := container.NewAppTabs(tabAboutThis(), tabAboutDeps(w))
	return container.NewTabItemWithIcon("About", theme.InfoIcon(), container.NewMax(subTabs))
}
