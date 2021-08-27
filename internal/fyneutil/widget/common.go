package widget

import (
	"go.polysender.org/internal/dbutil"
)

type Action struct {
	Name string
	Func func(v dbutil.Saveable, refreshChan chan<- struct{}) func()
}
