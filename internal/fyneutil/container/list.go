package page

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	widget2 "go.polysender.org/internal/fyneutil/widget"
)

func NewList(w fyne.Window, refreshChan chan struct{}, listActions []widget2.Action, loop func(refreshChan <-chan struct{}, t *widget2.List, noticeLabel *widget.Label)) *fyne.Container {
	noticeLabel := widget.NewLabel("")
	refreshNoticeContainer := container.NewHBox(
		widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
			go func() {
				refreshChan <- struct{}{}
			}()
		}),
		noticeLabel,
	)
	listWidget := widget2.NewList(refreshChan, listActions...)

	go loop(refreshChan, listWidget, noticeLabel)

	return container.NewBorder(refreshNoticeContainer, nil, nil, nil, container.NewScroll(listWidget))
}
