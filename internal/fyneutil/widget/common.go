package widget

import (
	"fyne.io/fyne/v2"
	"go.polysender.org/internal/dbutil"
)

type Action struct {
	Name string
	Icon fyne.Resource
	Func func(v dbutil.Saveable, refreshChan chan<- struct{}) func()
}
