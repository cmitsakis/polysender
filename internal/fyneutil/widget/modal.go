package widget

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func ShowModal(w fyne.Window, title, confirm, dismiss string, content fyne.CanvasObject, callback func() error) {
	var modal *widget.PopUp
	contentScroll := container.NewVScroll(content)
	top := container.NewVBox(
		widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
	)
	var bottom fyne.CanvasObject
	if confirm == "" {
		bottom = container.NewHBox(
			layout.NewSpacer(),
			widget.NewButtonWithIcon(dismiss, theme.CancelIcon(), func() {
				modal.Hide()
			}),
			layout.NewSpacer(),
		)
	} else {
		bottom = container.NewHBox(
			layout.NewSpacer(),
			widget.NewButtonWithIcon(dismiss, theme.CancelIcon(), func() {
				modal.Hide()
			}),
			widget.NewButtonWithIcon(confirm, theme.ConfirmIcon(), func() {
				err := callback()
				if err != nil {
					dialog.ShowError(err, w)
				} else {
					modal.Hide()
				}
			}),
			layout.NewSpacer(),
		)
	}
	contentBox := container.NewBorder(top, bottom, nil, nil, contentScroll)
	modal = widget.NewModalPopUp(contentBox, w.Canvas())
	// calculate the size of the modal by creating another temporary modal without scroll and getting it's size
	mSize := func() fyne.Size {
		tempContentBox := container.NewBorder(top, bottom, nil, nil, content)
		tempModal := widget.NewModalPopUp(tempContentBox, w.Canvas())
		s := tempContentBox.Size()
		tempModal.Hide()
		return fyne.Size{Height: s.Height + 10, Width: s.Width}
	}()
	mSize = mSize.Min(w.Canvas().Size())
	modal.Resize(mSize)
	modal.Show()
}
