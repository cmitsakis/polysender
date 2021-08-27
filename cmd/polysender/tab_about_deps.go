package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	widget2 "go.polysender.org/internal/fyneutil/widget"
	"go.polysender.org/internal/license"
)

func tabAboutDeps(w fyne.Window) *container.TabItem {
	items := make([]fyne.CanvasObject, 0)
	items = append(items, widget.NewLabel("Polysender includes the following third-party libraries"))
	for _, l := range license.Deps {
		lCopy := l
		items = append(items, container.NewHBox(widget.NewButton("License", func() {
			widget2.ShowModal(w, "License of "+lCopy.Package, "", "Close", widget.NewLabel(lCopy.License), nil)
		}), widget.NewLabel(l.Package)))
	}
	content := container.NewVBox(items...)
	return container.NewTabItemWithIcon("Third-Party Licenses", theme.InfoIcon(), container.NewScroll(content))
}
