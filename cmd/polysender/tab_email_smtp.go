package main

import (
	crand "crypto/rand"
	"fmt"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/oklog/ulid/v2"

	"go.polysender.org/internal/dbutil"
	container2 "go.polysender.org/internal/fyneutil/container"
	"go.polysender.org/internal/fyneutil/form"
	widget2 "go.polysender.org/internal/fyneutil/widget"
	"go.polysender.org/internal/gateway/email"
)

func tabEmailSMTP(w fyne.Window) *container.TabItem {
	refreshChan := make(chan struct{}, 1)

	newAccountBtn := widget.NewButtonWithIcon("New Account", theme.ContentAddIcon(), func() {
		fields := []form.FormField{
			{Name: "Host*"},
			{Name: "Port*"},
			{Name: "Username*"},
			{Name: "Password*"},
			{Name: "Connection Encryption*", Type: form.FormFieldTypeRadio, Options: []string{"TLS", "STARTTLS", "INSECURE"}},
			{Name: "Authentication*", Type: form.FormFieldTypeRadio, Options: []string{"PLAIN", "NONE"}, Description: "Select PLAIN. NONE is for testing purposes"},
			{Name: "HELO/EHLO host name", Description: "Optional. If not set, 'localhost' is used"},
			{Name: "Send limit per minute"},
			{Name: "Send limit per hour"},
			{Name: "Send limit per day", Description: "0 = no limit"},
			{Name: "SMTP connection reuse count limit", Description: "0 or 1 disables connection reuse.\n2+ will fail if the SMTP server\ndoes not support it."},
		}
		form.ShowFormPopup(w, "New SMTP Account", "Enter your SMTP server details", fields, func(inputValues []string) error {
			port, err := strconv.Atoi(inputValues[1])
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid port: %s", err))
			}
			if port < 0 || port > 65535 {
				return logAndReturnError(fmt.Errorf("invalid port: value should be between 0 and 65535"))
			}
			if inputValues[4] == "" {
				return logAndReturnError(fmt.Errorf("choose connection encryption"))
			}
			if inputValues[5] == "" {
				return logAndReturnError(fmt.Errorf("choose authentication type"))
			}
			var limitPerMinute uint64
			if inputValues[7] != "" {
				limitPerMinute, err = strconv.ParseUint(inputValues[7], 10, 32)
				if err != nil {
					return logAndReturnError(fmt.Errorf("limit per minute: invalid value: %s", err))
				}
			}
			var limitPerHour uint64
			if inputValues[8] != "" {
				limitPerHour, err = strconv.ParseUint(inputValues[8], 10, 32)
				if err != nil {
					return logAndReturnError(fmt.Errorf("limit per hour: invalid value: %s", err))
				}
				if limitPerHour > 0 && limitPerMinute == 0 {
					return logAndReturnError(fmt.Errorf("you cannot set limit per hour without setting limit per minute"))
				}
			}
			var limitPerDay uint64
			if inputValues[9] != "" {
				limitPerDay, err = strconv.ParseUint(inputValues[9], 10, 32)
				if err != nil {
					return logAndReturnError(fmt.Errorf("limit per day: invalid value': %s", err))
				}
				if limitPerDay > 0 && limitPerMinute == 0 {
					return logAndReturnError(fmt.Errorf("you cannot set limit per day without setting limit per minute"))
				}
			}
			var smtpConnectionReuseCountLimit uint64
			if inputValues[10] != "" {
				smtpConnectionReuseCountLimit, err = strconv.ParseUint(inputValues[10], 10, 32)
				if err != nil {
					return logAndReturnError(fmt.Errorf("SMTP connection reuse count limit: invalid value: %s", err))
				}
			}
			id, err := ulid.New(ulid.Timestamp(time.Now()), crand.Reader)
			if err != nil {
				return logAndReturnError(fmt.Errorf("Cannot create broadcast: %s", err))
			}
			a := email.SMTPAccount{
				ID:                        id,
				Host:                      inputValues[0],
				Port:                      port,
				Username:                  inputValues[2],
				Password:                  inputValues[3],
				ConnectionEncryption:      inputValues[4],
				AuthType:                  inputValues[5],
				HELOHost:                  inputValues[6],
				LimitPerMinute:            int(limitPerMinute),
				LimitPerHour:              int(limitPerHour),
				LimitPerDay:               int(limitPerDay),
				ConnectionReuseCountLimit: int(smtpConnectionReuseCountLimit),
			}
			loggerDebug.Println("[DEBUG] calling store.Save", a)
			err = dbutil.InsertSaveable(db, a)
			if err != nil {
				return logAndReturnError(fmt.Errorf("cannot write to database: %s", err))
			}
			refreshChan <- struct{}{}
			return nil
		})
	})

	tablePage := container2.NewTable(
		w,
		refreshChan,
		[]widget2.TableAttribute{
			{Name: "Actions", Actions: true},
			{Name: "ID", Field: "ID", Width: 270},
			{Name: "Host", Field: "Host", Width: 175},
			{Name: "Port", Field: "Port", Width: 75},
			{Name: "Username", Field: "Username", Width: 175},
		},
		[]widget2.Action{
			{
				Name: "Edit",
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						var a email.SMTPAccount
						err := dbutil.GetByKey(db, v.DBKey(), &a)
						if err != nil { // don't ignore dbutil.ErrNotFound
							logAndShowError(fmt.Errorf("database error: %s", err), w)
							return
						}
						fields := []form.FormField{
							{Name: "Host*", ExistingValue: a.Host},
							{Name: "Port*", ExistingValue: strconv.Itoa(a.Port)},
							{Name: "Username*", ExistingValue: a.Username},
							{Name: "Password*", ExistingValue: a.Password},
							{Name: "Connection Encryption*", Type: form.FormFieldTypeRadio, ExistingValue: a.ConnectionEncryption, Options: []string{"TLS", "STARTTLS", "INSECURE"}},
							{Name: "Authentication*", Type: form.FormFieldTypeRadio, ExistingValue: a.AuthType, Options: []string{"PLAIN", "NONE"}, Description: "Select PLAIN. NONE is for testing purposes"},
							{Name: "HELO/EHLO host name", ExistingValue: a.HELOHost, Description: "Optional. If not set, 'localhost' is used"},
							{Name: "Send limit per minute", ExistingValue: strconv.Itoa(a.LimitPerMinute)},
							{Name: "Send limit per hour", ExistingValue: strconv.Itoa(a.LimitPerHour)},
							{Name: "Send limit per day", ExistingValue: strconv.Itoa(a.LimitPerDay), Description: "0 = no limit"},
							{Name: "SMTP connection reuse count limit", ExistingValue: strconv.Itoa(a.ConnectionReuseCountLimit), Description: "0 or 1 disables connection reuse.\n2+ will fail if the SMTP server\ndoes not support it."},
						}
						form.ShowFormPopup(w, "Edit SMTP Account", "Enter your SMTP server details", fields, func(inputValues []string) error {
							port, err := strconv.Atoi(inputValues[1])
							if err != nil {
								return logAndReturnError(fmt.Errorf("invalid port: %s", err))
							}
							if port < 0 || port > 65535 {
								return logAndReturnError(fmt.Errorf("invalid port: value should be between 0 and 65535"))
							}
							if inputValues[4] == "" {
								return logAndReturnError(fmt.Errorf("choose connection encryption"))
							}
							if inputValues[5] == "" {
								return logAndReturnError(fmt.Errorf("choose authentication type"))
							}
							var limitPerMinute uint64
							if inputValues[7] != "" {
								limitPerMinute, err = strconv.ParseUint(inputValues[7], 10, 32)
								if err != nil {
									return logAndReturnError(fmt.Errorf("limit per minute: invalid value: %s", err))
								}
							}
							var limitPerHour uint64
							if inputValues[8] != "" {
								limitPerHour, err = strconv.ParseUint(inputValues[8], 10, 32)
								if err != nil {
									return logAndReturnError(fmt.Errorf("limit per hour: invalid value: %s", err))
								}
								if limitPerHour > 0 && limitPerMinute == 0 {
									return logAndReturnError(fmt.Errorf("you cannot set limit per hour without setting limit per minute"))
								}
							}
							var limitPerDay uint64
							if inputValues[9] != "" {
								limitPerDay, err = strconv.ParseUint(inputValues[9], 10, 32)
								if err != nil {
									return logAndReturnError(fmt.Errorf("limit per day: invalid value: %s", err))
								}
								if limitPerDay > 0 && limitPerMinute == 0 {
									return logAndReturnError(fmt.Errorf("you cannot set limit per day without setting limit per minute"))
								}
							}
							var smtpConnectionReuseCountLimit uint64
							if inputValues[10] != "" {
								smtpConnectionReuseCountLimit, err = strconv.ParseUint(inputValues[10], 10, 32)
								if err != nil {
									return logAndReturnError(fmt.Errorf("SMTP connection reuse count limit: invalid value: %s", err))
								}
							}
							a2 := email.SMTPAccount{
								ID:                        a.ID,
								Host:                      inputValues[0],
								Port:                      port,
								Username:                  inputValues[2],
								Password:                  inputValues[3],
								ConnectionEncryption:      inputValues[4],
								AuthType:                  inputValues[5],
								HELOHost:                  inputValues[6],
								LimitPerMinute:            int(limitPerMinute),
								LimitPerHour:              int(limitPerHour),
								LimitPerDay:               int(limitPerDay),
								ConnectionReuseCountLimit: int(smtpConnectionReuseCountLimit),
							}
							err = dbutil.UpsertSaveable(db, a2)
							if err != nil {
								return logAndReturnError(fmt.Errorf("cannot write to database: %s", err))
							}
							refreshChan <- struct{}{}
							return nil
						})
					}
				},
			}, {
				Name: "Delete",
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						content := widget.NewLabel("Are you sure you want to delete this SMTP server?")
						dialog.ShowCustomConfirm("Delete SMTP server", "Confirm", "Cancel", content, func(submit bool) {
							if submit {
								err := dbutil.DeleteByTableKey(db, v.DBTable(), v.DBKey())
								if err != nil {
									logAndShowError(fmt.Errorf("cannot write to database: %s", err), w)
								}
								refreshChan <- struct{}{}
							}
						}, w)
					}
				},
			},
		},
		func(refreshChan <-chan struct{}, t *widget2.Table, noticeLabel *widget.Label) {
			for range refreshChan {
				values := make([]dbutil.Saveable, 0)
				err := dbutil.ForEachReverse(db, &email.SMTPAccount{}, func(k []byte, v interface{}) error {
					vCasted, ok := v.(email.SMTPAccount)
					if !ok {
						return fmt.Errorf("value %v is not an SMTP account", v)
					}
					values = append(values, vCasted)
					return nil
				})
				t.UpdateAndRefresh(values)
				if err != nil {
					err = fmt.Errorf("cannot read from database: %s", err)
					loggerInfo.Println(err)
					noticeLabel.SetText(err.Error())
				} else {
					noticeLabel.SetText("")
				}
			}
		},
	)

	refreshChan <- struct{}{}

	content := container.NewBorder(newAccountBtn, nil, nil, nil, tablePage)
	return container.NewTabItemWithIcon("SMTP", theme.MailComposeIcon(), content)
}
