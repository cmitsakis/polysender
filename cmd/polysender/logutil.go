package main

import (
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

func logAndReturnError(err error) error {
	_, file, line, ok := runtime.Caller(1)
	if ok {
		logger.Printf("%s:%d: %s", file, line, err)
	} else {
		loggerInfo.Println(err)
	}
	return err
}

func logAndShowError(err error, w fyne.Window) {
	_, file, line, ok := runtime.Caller(1)
	if ok {
		logger.Printf("%s:%d: %s", file, line, err)
	} else {
		loggerInfo.Println(err)
	}
	dialog.ShowError(err, w)
}
