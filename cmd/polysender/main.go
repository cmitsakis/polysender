package main

import (
	crand "crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	mrand "math/rand"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/broadcast"
)

const (
	appID      = "org.polysender"
	appVersion = "0.2.0"
)

var (
	db          *bolt.DB
	loggerInfo  *log.Logger
	loggerDebug *log.Logger
	logger      = log.Default()
)

func main() {
	// random seed
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic("random number generator failed:" + err.Error())
	}
	mrand.Seed(int64(binary.LittleEndian.Uint64(b[:])))

	// read flags
	configDir, err := os.UserConfigDir()
	if err != nil {
		logger.Println("failed to locate config directory:", err)
		return
	}
	var (
		flagDebug   = flag.Bool("debug", false, "verbose output for debugging")
		flagVersion = flag.Bool("version", false, "Print version")
		flagHelp    = flag.Bool("help", false, "Print usage")
		flagDB      = flag.String("db", filepath.Join(configDir, appID, "data.db"), "path to database")
	)
	flag.Parse()
	switch {
	case *flagVersion:
		fmt.Println(appVersion)
		return
	case *flagHelp:
		flag.PrintDefaults()
		return
	}
	if *flagDebug {
		logger.Println("debug enabled")
		loggerInfo = log.New(os.Stdout, "[INFO] ", log.Ldate|log.Ltime|log.Llongfile|log.Lmsgprefix)
		loggerDebug = log.New(os.Stdout, "[DEBUG] ", log.Ldate|log.Ltime|log.Llongfile|log.Lmsgprefix)
	} else {
		loggerInfo = log.New(os.Stdout, "", log.Ldate|log.Ltime|log.Llongfile|log.Lmsgprefix)
		loggerDebug = log.New(ioutil.Discard, "", 0)
	}

	// create database directory if it does not exist
	if err := os.MkdirAll(filepath.Dir(*flagDB), 0700); err != nil {
		loggerInfo.Printf("failed to create directory '%s': %s", *flagDB, err)
		return
	}
	// open database
	db, err = bolt.Open(*flagDB, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		loggerInfo.Println("failed to open database:", err)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			loggerInfo.Println("failed to close database:", err)
		}
	}()

	// start dispatcher
	err = broadcast.DispatcherStart(db, loggerInfo, loggerDebug)
	if err != nil {
		loggerInfo.Println("failed to start dispatcher:", err)
		return
	}

	// start GUI
	a := app.NewWithID(appID)
	w := a.NewWindow("Polysender")
	w.SetMaster()
	a.Settings().SetTheme(theme.LightTheme())
	tabs := container.NewAppTabs(tabBroadcasts(w), tabSMSAndroid(w), tabEmail(w), tabAbout(w))
	w.SetContent(tabs)
	w.Resize(fyne.NewSize(1280, 720))
	w.ShowAndRun()
}
