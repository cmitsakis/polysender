package broadcast

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"text/template"
	"time"

	"github.com/oklog/ulid/v2"
	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/gateway"
	"go.polysender.org/internal/gateway/email"
	"go.polysender.org/internal/gateway/sms/android"
	"go.polysender.org/internal/workerpool"
)

type Run struct {
	BroadcastID ulid.ULID
	nextIndex   int
	broadcast   Broadcast
	gateway     gateway.Gateway
	done        map[int]struct{}
}

// Run implements dbutil.Saveable but it's not saved to database anymore
func (b Run) DBTable() string {
	return "broadcast.run"
}

func (b Run) DBKey() []byte {
	return b.BroadcastID[:]
}

func (r *Run) NextIndexGetAndIncrement() int {
	for i := r.nextIndex; i < len(r.broadcast.Contacts); i++ {
		_, exists := r.done[i]
		if !exists {
			r.nextIndex = i + 1
			return i
		}
	}
	return len(r.broadcast.Contacts)
}

func (r Run) NextIndexGet() int {
	for i := 0; i < len(r.broadcast.Contacts); i++ {
		_, exists := r.done[i]
		if !exists {
			return i
		}
	}
	return len(r.broadcast.Contacts)
}

var (
	tableNameEmailIdentity = new(email.Identity).DBTable()
	tableNameDeviceAndroid = new(android.Device).DBTable()
)

func newRunTx(tx *bolt.Tx, b Broadcast) (*Run, error) {
	r := Run{
		BroadcastID: b.ID,
		broadcast:   b,
		done:        make(map[int]struct{}),
	}
	err := dbutil.ForEachPrefixTx(tx, &Send{}, b.ID[:], func(k []byte, v interface{}) error {
		s := v.(Send)
		r.done[s.Index] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("dbutil.ForEachPrefixTx failed: %s", err)
	}
	if b.GatewayType == tableNameEmailIdentity {
		r.gateway, err = email.NewSMTPAccountFromKey(tx, b.GatewayKey)
		if err != nil {
			return nil, fmt.Errorf("cannot create sender client from key: %s error: %s", b.GatewayKey, err)
		}
	} else if b.GatewayType == tableNameDeviceAndroid {
		r.gateway, err = android.NewDeviceFromKey(tx, b.GatewayKey)
		if err != nil {
			return nil, fmt.Errorf("cannot create sender client from key: %s error: %s", b.GatewayKey, err)
		}
	} else {
		return nil, fmt.Errorf("unknown b.GatewayType %s", b.GatewayType)
	}
	return &r, nil
}

func newRun(db *bolt.DB, b Broadcast) (*Run, error) {
	var r *Run
	if err := db.View(func(tx *bolt.Tx) error {
		var err error
		r, err = newRunTx(tx, b)
		if err != nil {
			return fmt.Errorf("newRunTx failed: %s", err)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("database error: %s", err)
	}
	return r, nil
}

func newSenderClient(db *bolt.DB, r *Run) (gateway.SenderClient, error) {
	if r.broadcast.GatewayType == tableNameEmailIdentity {
		senderClient, err := email.NewSenderClientFromKey(db, r.broadcast.GatewayKey)
		if err != nil {
			return nil, fmt.Errorf("cannot create sender client from key: %s error: %s", r.broadcast.GatewayKey, err)
		}
		return senderClient, nil
	} else if r.broadcast.GatewayType == tableNameDeviceAndroid {
		senderClient, ok := r.gateway.(gateway.SenderClient)
		if !ok {
			return nil, fmt.Errorf("type assertion failed. r.gateway is not gateway.SenderClient")
		}
		return senderClient, nil
	}
	return nil, fmt.Errorf("unknown b.GatewayType %s", r.broadcast.GatewayType)
}

type Send struct {
	BroadcastID ulid.ULID
	Index       int
	Sent        int
	ErrorStr    string
}

func (b Send) DBTable() string {
	return "broadcast.send"
}

func (b Send) DBKey() []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(b.Index))
	return bytes.Join([][]byte{b.BroadcastID[:], buf}, nil)
}

func (b Send) String() string {
	var sentStr string
	switch b.Sent {
	case 0:
		sentStr = "NO"
	case 1:
		sentStr = "?"
	case 2:
		sentStr = "YES"
	default:
		sentStr = "invalid value"
	}
	var errorStr string
	if b.ErrorStr != "" {
		errorStr = ", error=" + b.ErrorStr
	}
	return fmt.Sprintf("contact #%d: sent=%s%s", b.Index+1, sentStr, errorStr)
}

func run(ctx context.Context, b Broadcast, db *bolt.DB, loggerInfo, loggerDebug *log.Logger, defaultSendHours SettingSendHours, defaultTimezone SettingTimezone) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	loggerDebugRun := log.New(loggerDebug.Writer(), loggerDebug.Prefix()+"[run] ", loggerDebug.Flags())
	bRun, err := newRun(db, b)
	if err != nil {
		return fmt.Errorf("broadcast %s could not be started - newRun() failed: %s", b.ID.String(), err)
	}

	// test connection
	senderClientTest, err := newSenderClient(db, bRun)
	if err != nil {
		return fmt.Errorf("newSenderClient() failed: %w", err)
	}
	err = senderClientTest.PreSend(ctx)
	if err != nil {
		return fmt.Errorf("preSend() failed: %w", err)
	}
	err = senderClientTest.PostSend(ctx)
	if err != nil {
		loggerDebugRun.Printf("PostSend() failed: %v\n", err)
	}

	msgTmplSubject, err := template.New("msg").Parse(bRun.broadcast.MsgSubject)
	if err != nil {
		return fmt.Errorf("template.Parse failed: %s", err)
	}
	msgTmplBody, err := template.New("msg").Parse(bRun.broadcast.MsgBody)
	if err != nil {
		return fmt.Errorf("template.Parse failed: %s", err)
	}
	var μ time.Duration
	if bRun.gateway.GetLimitPerMinute() > 0 {
		μ = time.Minute / time.Duration(bRun.gateway.GetLimitPerMinute())
	}
	q, err := workerpool.NewPoolWithInit(bRun.gateway.GetConcurrencyMax(), func(workerID int) (interface{}, error) {
		senderClient, err := newSenderClient(db, bRun)
		if err != nil {
			return nil, fmt.Errorf("newSenderClient() failed: %w", err)
		}
		err = senderClient.PreSend(ctx)
		if err != nil {
			return nil, fmt.Errorf("preSend() failed: %w", err)
		}
		return senderClient, nil
	}, func(workerID int, connection interface{}) error {
		senderClient := connection.(gateway.SenderClient)
		err := senderClient.PostSend(ctx)
		if err != nil {
			loggerDebugRun.Printf("PostSend() failed: %v\n", err)
		}
		return nil
	}, workerpool.Retries(4), workerpool.LoggerInfo(log.Default()), workerpool.LoggerDebug(log.Default()))
	if err != nil {
		return fmt.Errorf("failed to create pool of workers: %s", err)
	}
	defer q.StopAndWait()
	for i := bRun.NextIndexGetAndIncrement(); i < len(bRun.broadcast.Contacts); i = bRun.NextIndexGetAndIncrement() {
		i := i
		loggerDebugRunI := log.New(loggerDebug.Writer(), loggerDebugRun.Prefix()+fmt.Sprintf("[i=%d] ", i), loggerDebug.Flags())
		select {
		case <-ctx.Done():
			return fmt.Errorf("broadcast has stopped")
		default:
		}
	restart:
		// check if current time is within send hours
		if b.startableNowUntil(defaultSendHours, defaultTimezone).IsZero() {
			return fmt.Errorf("broadcast has stopped due to send hours")
		}

		// check limits
		sendCountsKeyCurrentMinute := []byte(b.GatewayType + string(b.GatewayKey) + time.Now().Truncate(time.Minute).Format("2006-01-02T15:04"))
		sendCountsKeyPastHour := []byte(b.GatewayType + string(b.GatewayKey) + time.Now().Add(-time.Hour).Truncate(time.Minute).Format("2006-01-02T15:04"))
		sendCountsKeyPastDay := []byte(b.GatewayType + string(b.GatewayKey) + time.Now().Add(-24*time.Hour).Truncate(time.Minute).Format("2006-01-02T15:04"))
		sendCountsKeyPrefix := []byte(b.GatewayType + string(b.GatewayKey))
		if bRun.gateway.GetLimitPerMinute() > 0 {
			sleepDur := time.Duration(float64(μ) * (1 + rand.ExpFloat64()) / 2)
			loggerDebugRunI.Printf("sleeping for %v\n", sleepDur)
			if sleepCtx(ctx, sleepDur) {
				return fmt.Errorf("broadcast has stopped")
			}
			// count sent in the last minute
			var count int
			err := dbutil.GetByTableKey(db, "send_counts", sendCountsKeyCurrentMinute, &count)
			if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
				return fmt.Errorf("failed to read count: %s", err)
			}
			if count >= bRun.gateway.GetLimitPerMinute() {
				// duration til minute changes
				sleepDur := time.Minute - time.Since(time.Now().Truncate(time.Minute))
				loggerDebugRunI.Printf("sent in the current minute %d - limit reached (%d) - sleeping for %v\n", count, bRun.gateway.GetLimitPerMinute(), sleepDur)
				if sleepCtx(ctx, sleepDur) {
					return fmt.Errorf("broadcast has stopped")
				}
				goto restart
			}
			loggerDebugRunI.Printf("sent in the current minute: %d\n", count)
		}
		if bRun.gateway.GetLimitPerHour() > 0 {
			// count sent in the last 60 minutes
			var count int
			err := dbutil.ForEachStartPrefix(db, "send_counts", sendCountsKeyPastHour, sendCountsKeyPrefix, &count, func(key []byte, val interface{}) error {
				count += val.(int)
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to read count: %s", err)
			}
			if count >= bRun.gateway.GetLimitPerHour() {
				return fmt.Errorf("sent in the last hour %d - limit reached (%d)", count, bRun.gateway.GetLimitPerHour())
			}
			loggerDebugRunI.Printf("sent in the last hour: %d\n", count)
		}
		if bRun.gateway.GetLimitPerDay() > 0 {
			// count sent in the last 24 hours
			var count int
			err := dbutil.ForEachStartPrefix(db, "send_counts", sendCountsKeyPastDay, sendCountsKeyPrefix, &count, func(key []byte, val interface{}) error {
				count += val.(int)
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to read count: %s", err)
			}
			if count >= bRun.gateway.GetLimitPerDay() {
				return fmt.Errorf("sent in the last 24 hours %d - limit reached (%d)", count, bRun.gateway.GetLimitPerDay())
			}
			loggerDebugRunI.Printf("sent in the last 24 hours: %d\n", count)
		}

		// generate message subject & body
		c := bRun.broadcast.Contacts[i]
		var bufSubject strings.Builder
		err = msgTmplSubject.Execute(&bufSubject, c.Keywords)
		if err != nil {
			return fmt.Errorf("msgTemplate.ExecuteTemplate failed: %s", err)
		}
		var bufBody strings.Builder
		err = msgTmplBody.Execute(&bufBody, c.Keywords)
		if err != nil {
			return fmt.Errorf("msgTemplate.ExecuteTemplate failed: %s", err)
		}

		q.Submit(func(workerID, attempt int, connection interface{}) error {
			senderClient := connection.(gateway.SenderClient)
			loggerDebugRunIA := log.New(loggerDebug.Writer(), loggerDebugRunI.Prefix()+fmt.Sprintf("[attempt=%d] ", attempt), loggerDebug.Flags())

			var sent int

			// send message
			loggerDebugRunIA.Printf("sending message to %v\n", c.Recipient)
			errSend := senderClient.Send(ctx, c.Recipient, bufSubject.String(), bufBody.String(), b.ID.String())
			// log if message was sent
			if errSend == nil {
				loggerDebugRunIA.Printf("message sent to %v\n", c.Recipient)
				sent = 2 // message sent
			} else if workerpool.ErrorIsRetryable(errSend) {
				loggerDebugRunIA.Printf("send failed with retryable error: %s\n", errSend)
				sent = 0 // message not sent
			} else {
				loggerDebugRunIA.Printf("send failed with non-retryable error: %s\n", errSend)
				sent = 1 // message maybe sent
			}

			// update DB
			errDB := db.Update(func(tx *bolt.Tx) error {
				var errStr string
				if errSend != nil {
					errStr = fmt.Sprintf("%s", errSend)
				}
				err = dbutil.UpsertSaveableTx(tx, Send{BroadcastID: b.ID, Index: i, Sent: sent, ErrorStr: errStr})
				if err != nil {
					return fmt.Errorf("failed to update Send: %s", err)
				}
				if sent == 0 {
					// message wasn't sent so don't count it
					return nil
				}

				// increase send_counts
				var count int
				err := dbutil.GetByTableKeyTx(tx, "send_counts", sendCountsKeyCurrentMinute, &count)
				if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
					return fmt.Errorf("failed to read count: %s", err)
				}
				err = dbutil.UpsertTableKeyValueTx(tx, "send_counts", sendCountsKeyCurrentMinute, count+1)
				if err != nil {
					return fmt.Errorf("failed to store count: %s", err)
				}
				return nil
			})
			// abort run on db failure
			if errDB != nil {
				return fmt.Errorf("failed to update database: %s", errDB)
			}

			// if send error, call PostSend() and PreSend() to find out if there is a connection issue
			if errSend != nil {
				err = senderClient.PostSend(ctx)
				if err != nil {
					loggerDebugRunIA.Printf("PostSend() failed: %v\n", err)
				}
				errPreSend := senderClient.PreSend(ctx)
				if errPreSend != nil {
					return fmt.Errorf("preSend() failed: %w", errPreSend)
				}
			}

			if sent == 0 {
				return fmt.Errorf("not sent")
			}
			// message was sent or maybe sent
			return nil
		})
	}
	loggerDebugRun.Println("run finished")
	return nil
}

func sleepCtx(ctx context.Context, dur time.Duration) bool {
	ticker := time.NewTicker(dur)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-ticker.C:
		return false
	}
}
