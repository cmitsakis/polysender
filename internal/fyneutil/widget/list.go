package widget

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"go.polysender.org/internal/dbutil"
)

type List struct {
	widget.List
	values      []dbutil.Saveable
	actions     []Action
	refreshChan chan<- struct{}
}

func NewList(refreshChan chan<- struct{}, actions ...Action) *List {
	l := &List{}
	l.refreshChan = refreshChan
	l.actions = actions
	l.Length = func() int {
		return 0
	}
	l.CreateItem = func() fyne.CanvasObject {
		c := container.NewHBox()
		for _, a := range actions {
			c.Add(widget.NewButton(a.Name, nil))
		}
		c.Add(widget.NewLabel(""))
		return c
	}
	l.List.UpdateItem = func(i int, item fyne.CanvasObject) {}
	l.ExtendBaseWidget(l)
	return l
}

func (l *List) UpdateAndRefresh(values []dbutil.Saveable) {
	l.values = values
	l.Length = func() int {
		return len(l.values)
	}
	l.UpdateItem = func(index int, item fyne.CanvasObject) {
		if index > len(l.values) {
			// item.Hide()
			return
		}
		// item.Show()
		value := l.values[index]
		for i, a := range l.actions {
			item.(*fyne.Container).Objects[i].(*widget.Button).OnTapped = a.Func(value, l.refreshChan)
		}
		item.(*fyne.Container).Objects[len(l.actions)].(*widget.Label).SetText(fmt.Sprintf("%v", value))
	}
	l.Refresh()
}
