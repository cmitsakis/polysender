package broadcast

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"text/template"
	"time"

	"github.com/oklog/ulid/v2"
	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/errorbehavior"
	"go.polysender.org/internal/gateway"
	"go.polysender.org/internal/gateway/email"
	"go.polysender.org/internal/gateway/sms/android"
)

type Run struct {
	BroadcastID ulid.ULID
	NextIndex   int
	broadcast   Broadcast
	gateway     gateway.Gateway
}

func (b Run) DBTable() string {
	return "broadcast.run"
}

func (b Run) DBKey() []byte {
	return b.BroadcastID[:]
}

var (
	tableNameEmailIdentity = new(email.Identity).DBTable()
	tableNameDeviceAndroid = new(android.Device).DBTable()
)

func newRun(db *bolt.DB, b Broadcast) (Run, error) {
	var existingRun Run
	err := dbutil.GetByKey(db, Run{BroadcastID: b.ID}.DBKey(), &existingRun)
	if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
		return Run{}, fmt.Errorf("cannot read broadcast run from database: %s", err)
	}
	r := Run{
		BroadcastID: b.ID,
		broadcast:   b,
		NextIndex:   existingRun.NextIndex,
	}
	if b.GatewayType == tableNameEmailIdentity {
		r.gateway, err = email.NewSMTPAccountFromKey(db, b.GatewayKey)
		if err != nil {
			return r, fmt.Errorf("cannot create sender client from key: %s error: %s", b.GatewayKey, err)
		}
	} else if b.GatewayType == tableNameDeviceAndroid {
		r.gateway, err = android.NewDeviceFromKey(db, b.GatewayKey)
		if err != nil {
			return r, fmt.Errorf("cannot create sender client from key: %s error: %s", b.GatewayKey, err)
		}
	} else {
		return r, fmt.Errorf("unknown b.GatewayType %s", b.GatewayType)
	}
	return r, nil
}

func newSenderClient(db *bolt.DB, r Run) (gateway.SenderClient, error) {
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

func run(ctx context.Context, b Broadcast, db *bolt.DB, loggerDebug *log.Logger, defaultSendHours SettingSendHours, defaultTimezone SettingTimezone) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	loggerDebugRun := log.New(loggerDebug.Writer(), loggerDebug.Prefix()+"[run] [broadcast: "+b.ID.String()+"] ", loggerDebug.Flags())
	bRun, err := newRun(db, b)
	if err != nil {
		return fmt.Errorf("broadcast %s could not be started - newRun() failed: %s", b.ID.String(), err)
	}
	senderClient, err := newSenderClient(db, bRun)
	if err != nil {
		return fmt.Errorf("newSenderClient() failed: %w", err)
	}
	err = senderClient.PreSend(ctx)
	if err != nil {
		return fmt.Errorf("preSend() failed: %w", err)
	}
	defer func() {
		err = senderClient.PostSend(ctx)
		if err != nil {
			loggerDebugRun.Printf("PostSend() failed: %v\n", err)
		}
	}()
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
	for i := bRun.NextIndex; i < len(bRun.broadcast.Contacts); i++ {
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

		for attempt := 0; attempt < 4; attempt++ {
			loggerDebugRunIA := log.New(loggerDebug.Writer(), loggerDebugRunI.Prefix()+fmt.Sprintf("[attempt=%d] ", attempt), loggerDebug.Flags())

			if attempt > 0 {
				μ2 := μ
				if μ == 0 {
					μ2 = time.Second
				}
				// sleep for 1*μ2, 4*μ2, 16*μ2 seconds
				sleepDur := time.Duration(math.Pow(4, float64(attempt-1))) * μ2
				loggerDebugRunIA.Printf("sleeping for %v\n", sleepDur)
				if sleepCtx(ctx, sleepDur) {
					return fmt.Errorf("broadcast has stopped")
				}
			}

			var sent int

			// send message
			loggerDebugRunIA.Printf("sending message to %v\n", c.Recipient)
			errSend := senderClient.Send(ctx, c.Recipient, bufSubject.String(), bufBody.String(), b.ID.String())
			// log if message was sent
			if errSend == nil {
				loggerDebugRunIA.Printf("message sent to %v\n", c.Recipient)
				sent = 2 // message sent
			} else if errorbehavior.IsRetryable(errSend) {
				loggerDebugRunIA.Printf("send failed with retryable error: %s\n", errSend)
				sent = 0 // message not sent
			} else {
				loggerDebugRunIA.Printf("send failed with non-retryable error: %s\n", errSend)
				sent = 1 // message maybe sent
			}

			// update DB
			bRun.NextIndex = i + 1
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

				// update broadcast run
				err = dbutil.UpsertSaveableTx(tx, bRun)
				if err != nil {
					return fmt.Errorf("failed to store run: %s", err)
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
				// abort run on PreSend() failure
				if errPreSend != nil {
					return fmt.Errorf("preSend() failed: %w", errPreSend)
				}
			}

			// exit loop if message was sent or maybe sent
			if sent > 0 {
				break
			}
		}
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
