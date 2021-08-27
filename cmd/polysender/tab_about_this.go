package main

import (
	"fmt"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func tabAboutThis() *container.TabItem {
	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Polysender version %s", appVersion)),
		widget.NewLabel("Copyright (C) 2021 Charalampos Mitsakis"),
		widget.NewLabel("Polysender is licensed under the terms of the PolyForm Internal Use License 1.0.0 https://polyformproject.org/licenses/internal-use/1.0.0"),
		widget.NewLabel("Third-party contributions are licensed under the terms of the Blue Oak Model License 1.0.0 https://blueoakcouncil.org/license/1.0.0"),
		widget.NewLabel("As far as the law allows, the software comes as is, without any warranty or condition, and the licensor will not be liable to you for any damages arising out of these terms or the use or nature of the software, under any kind of legal claim."),
	)
	return container.NewTabItemWithIcon("Polysender", theme.InfoIcon(), container.NewScroll(content))
}
