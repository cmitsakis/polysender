package broadcast

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/gateway/sms/android"
)

var (
	runningBroadcasts map[string]struct{}
	runningGateways   map[string]struct{}
	runningMutex      sync.Mutex
)

func init() {
	runningBroadcasts = make(map[string]struct{})
	runningGateways = make(map[string]struct{})
}

func Dispatcher(ctx context.Context, db *bolt.DB, loggerInfo *log.Logger, loggerDebug *log.Logger) {
	loggerDebug2 := log.New(loggerDebug.Writer(), loggerDebug.Prefix()+"[Dispatcher] ", loggerDebug.Flags())
	loggerInfo2 := log.New(loggerInfo.Writer(), loggerInfo.Prefix()+"[Dispatcher] ", loggerInfo.Flags())
	for {
		time.Sleep(60 * time.Second)
		var bs []Broadcast
		err := dbutil.ForEach(db, &Broadcast{}, func(k []byte, v interface{}) error {
			b := v.(Broadcast)
			// TODO: check if broadcast can be started?
			bs = append(bs, b)
			return nil
		})
		if err != nil {
			loggerInfo2.Println("failed to read broadcasts from database:", err)
			continue
		}
		// read settings
		var defaultSendHours SettingSendHours
		err = dbutil.GetByKey(db, defaultSendHours.DBKey(), &defaultSendHours)
		if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
			loggerInfo2.Println("failed to read send hours settings from database:", err)
			continue
		}
		var defaultTimezone SettingTimezone
		err = dbutil.GetByKey(db, defaultTimezone.DBKey(), &defaultTimezone)
		if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
			loggerInfo2.Println("failed to read time zone settings from database:", err)
			continue
		}
		// find startable broadcasts
		sort.Sort(broadcastsByStartableSince{Broadcasts: bs, SendHours: defaultSendHours, Timezone: defaultTimezone})
		bsToStart := make([]Broadcast, 0, len(bs))
		gatewaysToStart := make(map[string]struct{})
		for _, b := range bs {
			loggerDebugB := log.New(loggerDebug2.Writer(), loggerDebug2.Prefix()+fmt.Sprintf("[broadcast: %s] ", b.ID.String()), loggerDebug2.Flags())
			// loggerInfoB := log.New(loggerInfo2.Writer(), loggerInfo2.Prefix()+fmt.Sprintf("[broadcast: %s] ", b.ID.String()), loggerInfo2.Flags())

			// check if broadcast can be started
			if b.startableNowUntil(defaultSendHours, defaultTimezone).IsZero() {
				loggerDebugB.Println("broadcast cannot be started now - ignoring")
				continue
			}

			// remove broadcasts using the same gateway
			_, exists := gatewaysToStart[b.GatewayType+string(b.GatewayKey)]
			if exists {
				loggerDebugB.Printf("gateway %s already scheduled to start - ignoring\n", b.GatewayKey)
				continue
			}
			_, exists = runningGateways[b.GatewayType+string(b.GatewayKey)]
			if exists {
				loggerDebugB.Printf("gateway %s currently in use - ignoring\n", b.GatewayKey)
				continue
			}

			// check if broadcast and gateway is already running
			var existsB bool
			var existsG bool
			func() {
				runningMutex.Lock()
				defer runningMutex.Unlock()
				_, existsB = runningBroadcasts[b.ID.String()]
				_, existsG = runningGateways[b.GatewayType+string(b.GatewayKey)]
			}()
			if existsB {
				loggerDebugB.Println("broadcast has already been started - ignoring")
				continue
			}
			if existsG {
				loggerDebugB.Println("broadcast's gateway already in use - ignoring")
				continue
			}

			// check if broadcast has finished
			var r Run
			err = dbutil.GetByKey(db, Run{BroadcastID: b.ID}.DBKey(), &r)
			if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
				loggerDebugB.Println("dbutil.GetByKeyarray failed")
				continue
			}
			// log.Printf("[DEBUG] [Dispatcher] r.NextIndex: %d r.Length: %d err: %s\n", r.NextIndex, r.Length, err)
			if err == nil && r.NextIndex == len(b.Contacts) {
				loggerDebugB.Println("broadcast has finished - ignoring")
				continue
			}

			gatewaysToStart[b.GatewayType+string(b.GatewayKey)] = struct{}{}

			bsToStart = append(bsToStart, b)
		}
		for _, b := range bsToStart {
			// loggerDebugB := log.New(loggerDebug2.Writer(), loggerDebug2.Prefix()+fmt.Sprintf("[broadcast: %s] ", b.ID.String()), loggerDebug2.Flags())
			loggerInfoB := log.New(loggerInfo2.Writer(), loggerInfo2.Prefix()+fmt.Sprintf("[broadcast: %s] ", b.ID.String()), loggerInfo2.Flags())
			func() {
				runningMutex.Lock()
				defer runningMutex.Unlock()
				runningBroadcasts[b.ID.String()] = struct{}{}
				runningGateways[b.GatewayType+string(b.GatewayKey)] = struct{}{}
			}()
			loggerInfoB.Println("broadcast starting")
			go func(b Broadcast) {
				defer func() {
					runningMutex.Lock()
					defer runningMutex.Unlock()
					delete(runningBroadcasts, b.ID.String())
					delete(runningGateways, b.GatewayType+string(b.GatewayKey))
				}()
				err := run(ctx, b, db, loggerDebug, defaultSendHours, defaultTimezone)
				if err != nil {
					loggerInfoB.Printf("broadcast stopped: %s\n", err)
					if errors.Is(err, android.ErrDeviceUnreachable) {
						fyne.CurrentApp().SendNotification(&fyne.Notification{
							Title:   "[Polysender] Broadcast stopped. Android device is unreachable",
							Content: "Connect the Android device " + string(b.GatewayKey) + " via ADB or KDE Connect",
						})
					}
				} else {
					loggerInfoB.Println("broadcast finished")
				}
			}(b)
		}
		func() {
			runningMutex.Lock()
			defer runningMutex.Unlock()
			runningBroadcastsKeys := make([]string, 0, len(runningBroadcasts))
			for runningBroadcastKey := range runningBroadcasts {
				runningBroadcastsKeys = append(runningBroadcastsKeys, runningBroadcastKey)
			}
			loggerDebug2.Printf("%v broadcasts are currently running: %v\n", len(runningBroadcasts), runningBroadcastsKeys)
		}()
	}
}
