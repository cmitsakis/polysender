package page

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	widget2 "go.polysender.org/internal/fyneutil/widget"
)

func NewTable(w fyne.Window, refreshChan chan struct{}, tableAttrs []widget2.TableAttribute, tableActions []widget2.Action, loop func(refreshChan <-chan struct{}, t *widget2.Table, noticeLabel *widget.Label)) *fyne.Container {
	noticeLabel := widget.NewLabel("")
	refreshNoticeContainer := container.NewHBox(
		widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
			go func() {
				refreshChan <- struct{}{}
			}()
		}),
		noticeLabel,
	)
	tableWidget := widget2.NewTable(refreshChan, tableAttrs, tableActions...)

	go loop(refreshChan, tableWidget, noticeLabel)

	return container.NewBorder(refreshNoticeContainer, nil, nil, nil, container.NewScroll(tableWidget))
}
