package main

import (
	"bufio"
	crand "crypto/rand"
	"fmt"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/oklog/ulid/v2"
	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/broadcast"
	"go.polysender.org/internal/dbutil"
	container2 "go.polysender.org/internal/fyneutil/container"
	"go.polysender.org/internal/fyneutil/form"
	widget2 "go.polysender.org/internal/fyneutil/widget"
	"go.polysender.org/internal/gateway/email"
	"go.polysender.org/internal/gateway/sms/android"
	"go.polysender.org/internal/tzdb"
)

var (
	broadcastsDirectoryContacts string
	broadcastsDirectoryCopy     string
	broadcastsDirectoryMutex    sync.Mutex
)

func tabBroadcastsSendQueue(w fyne.Window) *container.TabItem {
	refreshChan := make(chan struct{}, 1)
	newBroadcastBtn := widget.NewButtonWithIcon("New Broadcast", theme.ContentAddIcon(), func() {
		showBroadcastWizard1(w, refreshChan)
	})
	queueStatusLabel := widget.NewLabel("")
	stopBtn := widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
		go func() {
			err := broadcast.DispatcherStop(loggerInfo, loggerDebug)
			if err != nil {
				logAndShowError(fmt.Errorf("failed to stop dispatcher: %s", err), w)
			}
		}()
	})
	startBtn := widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), func() {
		go func() {
			err := broadcast.DispatcherStart(db, loggerInfo, loggerDebug)
			if err != nil {
				logAndShowError(fmt.Errorf("failed to start dispatcher: %s", err), w)
			}
		}()
	})
	go func() {
		for status := range broadcast.DispatcherStatusChan {
			queueStatusLabel.SetText("Status: " + status.String())
		}
	}()
	tablePage := container2.NewTable(
		w,
		refreshChan,
		[]widget2.TableAttribute{
			{Name: "Actions", Actions: true},
			{Name: "ID", Field: "ID", Width: 270},
			{Name: "Status", Field: "GetStatus", Width: 170},
			{Name: "Message Subject", Field: "MsgSubject", Width: 220},
			{Name: "Message Body", Field: "MsgBody", Width: 300},
		},
		[]widget2.Action{
			{
				Icon: theme.InfoIcon(),
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						var details string
						if err := db.View(func(tx *bolt.Tx) error {
							var b broadcast.Broadcast
							err := dbutil.GetByKeyTx(tx, v.DBKey(), &b)
							if err != nil { // don't ignore dbutil.ErrNotFound
								return fmt.Errorf("dbutil.GetByKeyTx failed: %s", err)
							}
							details, err = b.DetailsString(tx)
							if err != nil {
								return fmt.Errorf("b.DetailsString failed: %s", err)
							}
							return nil
						}); err != nil {
							logAndShowError(fmt.Errorf("database error: %s", err), w)
							return
						}
						contentView := widget.NewLabel(details)
						dialog.ShowCustom("Broadcast Details", "Close", contentView, w)
					}
				},
			}, {
				Icon: theme.MailComposeIcon(),
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						var bSends []broadcast.Send
						if err := db.View(func(tx *bolt.Tx) error {
							var b broadcast.Broadcast
							err := dbutil.GetByKeyTx(tx, v.DBKey(), &b)
							if err != nil { // don't ignore dbutil.ErrNotFound
								return fmt.Errorf("dbutil.GetByKeyTx failed: %s", err)
							}
							err = dbutil.ForEachPrefixTx(tx, &broadcast.Send{}, b.ID[:], func(k []byte, v interface{}) error {
								vCasted, ok := v.(broadcast.Send)
								if !ok {
									return fmt.Errorf("value %v is not a broadcast send", v)
								}
								bSends = append(bSends, vCasted)
								return nil
							})
							if err != nil {
								return fmt.Errorf("dbutil.ForEachPrefixTx failed: %s", err)
							}
							return nil
						}); err != nil {
							logAndShowError(fmt.Errorf("database error: %s", err), w)
							return
						}
						var buf strings.Builder
						for _, bSend := range bSends {
							buf.WriteString(bSend.String() + "\n")
						}
						widget2.ShowModal(w, "Sent", "", "Close", widget.NewLabel(buf.String()), nil)
					}
				},
			}, {
				Icon: theme.DeleteIcon(),
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						content := widget.NewLabel("Are you sure you want to delete this broadcast?")
						dialog.ShowCustomConfirm("Delete Broadcast", "Confirm", "Cancel", content, func(submit bool) {
							if submit {
								if err := db.Update(func(tx *bolt.Tx) error {
									err := dbutil.DeleteByTableKeyTx(tx, broadcast.Broadcast{}.DBTable(), v.DBKey())
									if err != nil {
										return fmt.Errorf("failed to delete Broadcast: %s", err)
									}
									err = dbutil.DeleteByTableKeyTx(tx, broadcast.Run{}.DBTable(), v.DBKey())
									if err != nil {
										return fmt.Errorf("failed to delete Run: %s", err)
									}
									err = dbutil.DeletePrefixTx(tx, broadcast.Send{}.DBTable(), v.DBKey())
									if err != nil {
										return fmt.Errorf("failed to delete Send: %s", err)
									}
									return nil
								}); err != nil {
									logAndShowError(fmt.Errorf("database error: %s", err), w)
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
				err := db.View(func(tx *bolt.Tx) error {
					err := dbutil.ForEachReverseTx(tx, &broadcast.Broadcast{}, func(k []byte, v interface{}) error {
						vCasted, ok := v.(broadcast.Broadcast)
						if !ok {
							return fmt.Errorf("value %v is not a broadcast", v)
						}
						err := vCasted.ReadStatusFromTx(tx)
						if err != nil {
							return fmt.Errorf("vCasted.ReadStatusFromTx() failed: %s", err)
						}
						values = append(values, vCasted)
						return nil
					})
					if err != nil {
						return err
					}
					return nil
				})
				if err != nil {
					err = fmt.Errorf("cannot read broadcast: %s", err)
					loggerInfo.Println(err)
					noticeLabel.SetText(err.Error())
				} else {
					noticeLabel.SetText("")
				}
				t.UpdateAndRefresh(values)
				func() {
					queueStatusLabel.SetText("Status: " + broadcast.DispatcherGetStatus().String())
				}()
			}
		},
	)
	refreshChan <- struct{}{}
	top := container.NewVBox(newBroadcastBtn, container.NewHBox(queueStatusLabel, stopBtn, startBtn))
	content := container.NewBorder(top, nil, nil, nil, tablePage)
	return container.NewTabItemWithIcon("Send Queue", theme.MailSendIcon(), content)
}

func showBroadcastWizard1(w fyne.Window, refreshChan chan struct{}) {
	var m sync.Mutex
	var fileStringBuilder strings.Builder
	var filename string
	delimiterEntry := widget.NewEntry()
	delimiterEntry.SetPlaceHolder("empty = comma")
	hasHeaderCheck := widget.NewCheck("", nil)
	recipientColumnEntry := widget.NewEntry()
	recipientColumnEntry.SetPlaceHolder("e.g. 'email' or '2'")
	optionFileTypeSingleColumn := ".txt (single column)"
	optionFileTypeCSV := ".csv (multiple columns)"
	fileTypeRadio := widget.NewRadioGroup([]string{optionFileTypeSingleColumn, optionFileTypeCSV}, func(selection string) {
		if selection == optionFileTypeSingleColumn {
			delimiterEntry.Disable()
			hasHeaderCheck.Disable()
			recipientColumnEntry.Disable()
		} else if selection == optionFileTypeCSV {
			delimiterEntry.Enable()
			hasHeaderCheck.Enable()
			recipientColumnEntry.Enable()
		}
	})
	fileTypeRadio.Required = true
	fileBtn := widget.NewButtonWithIcon("File (.txt or .csv)", theme.FileIcon(), func() {
		go func() {
			d := dialog.NewFileOpen(func(file fyne.URIReadCloser, err error) {
				if err != nil {
					logAndShowError(fmt.Errorf("Failed to select file: %s", err), w)
					return
				}
				if file == nil {
					// user clicked "Cancel"
					return
				}
				go func() {
					// lock mutex because we write to fileStringBuilder. is it needed?
					m.Lock()
					defer m.Unlock()
					filename = file.URI().Name()
					scanner := bufio.NewScanner(file)
					fileStringBuilder.Reset()
					for scanner.Scan() {
						fileStringBuilder.WriteString(scanner.Text())
						fileStringBuilder.WriteString("\n")
					}
					if err := scanner.Err(); err != nil {
						logAndShowError(fmt.Errorf("Error reading file: %s", err), w)
						return
					}
					loggerDebug.Println("uploaded file:", filename)
					if strings.HasSuffix(filename, ".txt") {
						fileTypeRadio.SetSelected(optionFileTypeSingleColumn)
					} else if strings.HasSuffix(filename, ".csv") {
						fileTypeRadio.SetSelected(optionFileTypeCSV)
					}

					// remember directory
					broadcastsDirectoryMutex.Lock()
					defer broadcastsDirectoryMutex.Unlock()
					broadcastsDirectoryContacts = filepath.Dir(file.URI().Path())
					loggerDebug.Println("set broadcastsDirectoryContacts =", broadcastsDirectoryContacts)
				}()
			}, w)
			d.SetFilter(storage.NewExtensionFileFilter([]string{".txt", ".csv"}))

			// set location to remembered directory
			broadcastsDirectoryMutex.Lock()
			defer broadcastsDirectoryMutex.Unlock()
			lister, err := storage.ListerForURI(storage.NewFileURI(broadcastsDirectoryContacts))
			if err != nil {
				loggerDebug.Printf("failed to read broadcastsDirectoryContacts: %s\n", err)
			} else {
				d.SetLocation(lister)
			}

			d.Show()
		}()
	})
	f := &widget.Form{}
	f.Append("Contacts:", fileBtn)
	f.Append("File type:", fileTypeRadio)
	f.Append("CSV Delimiter:", delimiterEntry)
	f.Append("CSV has header:", hasHeaderCheck)
	f.Append("Recipient column in CSV:", recipientColumnEntry)
	f.Append("", widget.NewLabelWithStyle("header name or number", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	form.ShowCustomPopup(w, "New Broadcast - Step 1/2", "", "Next", "Cancel", f, func() error {
		// lock mutex because we read from fileStringBuilder
		m.Lock()
		defer m.Unlock()
		fileContent := fileStringBuilder.String()
		if fileContent == "" {
			return logAndReturnError(fmt.Errorf("Please select a file first"))
		}
		var err error
		var contacts []broadcast.Contact
		if fileTypeRadio.Selected == optionFileTypeSingleColumn {
			contacts, err = broadcast.ReadContactsFromReader(strings.NewReader(fileContent))
			if err != nil {
				return logAndReturnError(fmt.Errorf("Error reading file: %s", err))
			}
		} else if fileTypeRadio.Selected == optionFileTypeCSV {
			// input: delimiter
			var delimiter rune
			if len(delimiterEntry.Text) > 0 {
				delimiter = rune(delimiterEntry.Text[0])
			}

			// input: hasHeader
			hasHeader := hasHeaderCheck.Checked

			// input: recipient column
			var recipientColumnInt int
			var recipientColumnStr string
			recipientColumnEntryStr := recipientColumnEntry.Text
			if recipientColumnEntryStr != "" {
				recipientColumnInt, err = strconv.Atoi(recipientColumnEntryStr)
				if err != nil {
					recipientColumnStr = recipientColumnEntryStr
				}
			}

			// input: file
			contacts, err = broadcast.ReadContactsFromReaderCSV(strings.NewReader(fileContent), delimiter, hasHeader, recipientColumnStr, recipientColumnInt)
			if err != nil {
				return logAndReturnError(fmt.Errorf("Cannot read contacts from file: %s", err))
			}
			if len(contacts) == 0 {
				return logAndReturnError(fmt.Errorf("no contacts found on file"))
			}
		} else {
			return logAndReturnError(fmt.Errorf("Please select file type"))
		}
		// start new goroutine, otherwise it won't show
		go func(w fyne.Window, filename string, contacts []broadcast.Contact, refreshChan chan struct{}) {
			showBroadcastWizard2(w, filename, contacts, refreshChan)
		}(w, filename, contacts, refreshChan)
		return nil
	})
}

func showBroadcastWizard2(w fyne.Window, filename string, contacts []broadcast.Contact, refreshChan chan struct{}) {
	var m sync.Mutex

	msgSubjectInput := widget.NewEntry()
	msgSubjectInput.SetPlaceHolder("Subject (leave empty for SMS)")
	msgSubjectExample := widget.NewLabel("")
	msgSubjectInput.OnChanged = func(input string) {
		// should I lock mutex before reading msgSubjectInput.Text?
		msgInputText := msgSubjectInput.Text
		msgTmpl, err := template.New("msg").Parse(msgInputText)
		if err != nil {
			loggerDebug.Printf("failed to parse msgInput.Text(%s)\n", msgInputText)
			msgSubjectExample.SetText("invalid syntax")
			return
		}
		msg, err := generateMessageRandomContact(contacts, msgTmpl)
		if err != nil {
			msgSubjectExample.SetText(fmt.Sprintf("message generation failed: %s", err))
			return
		}
		msgSubjectExample.SetText(msg)
	}

	msgBodyExample := widget.NewLabel("")
	msgBodyExample.Wrapping = fyne.TextWrapBreak
	var msgBodyFileStringBuilder strings.Builder
	msgBodyFileBtn := widget.NewButtonWithIcon("File (.txt)", theme.FileIcon(), func() {
		go func() {
			d := dialog.NewFileOpen(func(file fyne.URIReadCloser, err error) {
				if err != nil {
					logAndShowError(fmt.Errorf("Failed to select file: %s", err), w)
					return
				}
				if file == nil {
					// user clicked "Cancel"
					return
				}
				go func() {
					// lock mutex because we write to msgBodyFileStringBuilder
					m.Lock()
					defer m.Unlock()
					scanner := bufio.NewScanner(file)
					msgBodyFileStringBuilder.Reset()
					for scanner.Scan() {
						msgBodyFileStringBuilder.WriteString(scanner.Text())
						msgBodyFileStringBuilder.WriteString("\n")
					}
					if err := scanner.Err(); err != nil {
						logAndShowError(fmt.Errorf("Error reading file: %s", err), w)
						return
					}
					msgTmpl, err := template.New("msg").Parse(msgBodyFileStringBuilder.String())
					if err != nil {
						loggerDebug.Println("failed to parse msgBodyFileStringBuilder.String()")
						msgBodyExample.SetText("invalid syntax")
						return
					}
					msg, err := generateMessageRandomContact(contacts, msgTmpl)
					if err != nil {
						msgBodyExample.SetText(fmt.Sprintf("message generation failed: %s", err))
						return
					}
					msgBodyExample.SetText(msg)

					// remember directory
					broadcastsDirectoryMutex.Lock()
					defer broadcastsDirectoryMutex.Unlock()
					broadcastsDirectoryCopy = filepath.Dir(file.URI().Path())
					loggerDebug.Println("set broadcastsDirectoryCopy =", broadcastsDirectoryCopy)
				}()
			}, w)
			d.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))

			// set location to remembered directory
			broadcastsDirectoryMutex.Lock()
			defer broadcastsDirectoryMutex.Unlock()
			lister, err := storage.ListerForURI(storage.NewFileURI(broadcastsDirectoryCopy))
			if err != nil {
				loggerDebug.Printf("failed to read broadcastsDirectoryCopy: %s - trying broadcastsDirectoryContacts\n", err)
				lister, err := storage.ListerForURI(storage.NewFileURI(broadcastsDirectoryContacts))
				if err != nil {
					loggerDebug.Printf("failed to read broadcastsDirectoryContacts: %s\n", err)
				} else {
					d.SetLocation(lister)
				}
			} else {
				d.SetLocation(lister)
			}

			d.Show()
		}()
	})

	gateways := make([]dbutil.Saveable, 0)
	err := dbutil.ForEach(db, &email.Identity{}, func(k []byte, v interface{}) error {
		emailID := v.(email.Identity)
		gateways = append(gateways, emailID)
		return nil
	})
	if err != nil {
		logAndShowError(fmt.Errorf("cannot read email identities from database: %s", err), w)
	}
	err = dbutil.ForEach(db, &android.Device{}, func(k []byte, v interface{}) error {
		dev := v.(android.Device)
		gateways = append(gateways, dev)
		return nil
	})
	if err != nil {
		logAndShowError(fmt.Errorf("cannot read android devices from database: %s", err), w)
	}
	gatewayStrings := make([]string, 0, len(gateways))
	for _, g := range gateways {
		gatewayStrings = append(gatewayStrings, fmt.Sprintf("%v", g))
	}
	var gatewaySelected dbutil.Saveable
	gatewaySelect := widget.NewSelect(gatewayStrings, func(selected string) {
	})

	sendHoursEntry := widget.NewEntry()
	sendHoursEntry.SetPlaceHolder("Time ranges in 24 hour format e.g. 9-13 15-17")
	var timezoneSelected string
	timezoneValue := form.NewValue(w, "Optional. If not set, value from settings is used", func(labelUpdates chan<- string) {
		form.ShowEntryCompletionPopup(w, "Time zone", "Search by country or time zone and select a result", "", "", tzdb.TimeZones, form.FilterOptions, func(inputText string) error {
			fields := strings.Fields(inputText)
			if len(fields) == 0 {
				return logAndReturnError(fmt.Errorf("invalid time zone"))
			}
			tzName := fields[0]
			_, err := time.LoadLocation(tzName)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid time zone: %s", err))
			}
			labelUpdates <- tzName
			// lock mutex because we write to timezoneSelected
			m.Lock()
			defer m.Unlock()
			timezoneSelected = tzName
			return nil
		})
	}, func(labelUpdates chan<- string) {
		labelUpdates <- ""
		// lock mutex because we write to timezoneSelected
		m.Lock()
		defer m.Unlock()
		timezoneSelected = ""
	})

	sendDate1Entry := widget.NewEntry()
	sendDate1Entry.SetPlaceHolder("e.g. 2021-08-16")

	sendDate2Entry := widget.NewEntry()
	sendDate2Entry.SetPlaceHolder("e.g. 2021-08-16")

	f := &widget.Form{}
	f.Append("Subject:", msgSubjectInput)
	f.Append("Subject example:", msgSubjectExample)
	f.Append("Message body:", msgBodyFileBtn)
	f.Append("Message example:", msgBodyExample)
	f.Append("Gateway:", gatewaySelect)
	f.Append("Send hours:", sendHoursEntry)
	f.Append("", widget.NewLabelWithStyle("Optional. If not set, value from settings is used", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	f.Append("Time zone:", timezoneValue)
	f.Append("Send date start:", sendDate1Entry)
	f.Append("", widget.NewLabelWithStyle("Optional. If you want the broadcast to start at a specific\nday in the future", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	f.Append("Send date end:", sendDate2Entry)
	f.Append("", widget.NewLabelWithStyle("Optional. If you want broadcast to stop at at a specific date\neven if not all recipients have been contacted.\nUseful for time-sensitive announcements.", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))

	form.ShowCustomPopup(w, "New Broadcast - Step 2/2", "", "Next", "Cancel", f, func() error {
		// lock mutex because we read from msgBodyFileStringBuilder, timezoneSelected
		m.Lock()
		defer m.Unlock()
		msgSubject := msgSubjectInput.Text
		_, err := template.New("msg_subject").Parse(msgSubject)
		if err != nil {
			return logAndReturnError(fmt.Errorf("failed to parse message subject: %s", err))
		}
		_, err = template.New("msg_body").Parse(msgBodyFileStringBuilder.String())
		if err != nil {
			return logAndReturnError(fmt.Errorf("failed to parse message body: %s", err))
		}
		var sendHours broadcast.TimeRanges
		if sendHoursEntry.Text != "" {
			sendHours, err = broadcast.ParseTimeRanges(sendHoursEntry.Text)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid time ranges: %s", err))
			}
		}
		var timezoneSelected2 string
		var loc *time.Location
		if timezoneSelected != "" {
			fields := strings.Fields(timezoneSelected)
			if len(fields) == 0 {
				return logAndReturnError(fmt.Errorf("invalid time zone"))
			}
			tzName := fields[0]
			loc, err = time.LoadLocation(tzName)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid time zone: %s", err))
			}
			timezoneSelected2 = tzName
		}
		if loc == nil {
			loc = time.Now().Location()
		}
		var sendDate1 time.Time
		if sendDate1Entry.Text != "" {
			sendDate1, err = time.ParseInLocation("2006-01-02", sendDate1Entry.Text, loc)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid date: %s", err))
			}
			loggerDebug.Println("dateFrom parsed:", sendDate1)
		}
		var sendDate2 time.Time
		if sendDate2Entry.Text != "" {
			sendDate2, err = time.ParseInLocation("2006-01-02", sendDate2Entry.Text, loc)
			if err != nil {
				return logAndReturnError(fmt.Errorf("invalid date: %s", err))
			}
		}
		loggerDebug.Printf("gatewaySelect.SelectedIndex(): %v\n", gatewaySelect.SelectedIndex())
		gatewaySelectedIndex := gatewaySelect.SelectedIndex()
		if gatewaySelectedIndex > -1 {
			gatewaySelected = gateways[gatewaySelectedIndex]
			loggerDebug.Println("gateway selected:", gatewaySelected)
		} else {
			return logAndReturnError(fmt.Errorf("Please select a gateway"))
		}
		id, err := ulid.New(ulid.Timestamp(time.Now()), crand.Reader)
		if err != nil {
			return logAndReturnError(fmt.Errorf("Cannot create broadcast: %s", err))
		}
		b := broadcast.Broadcast{
			ID:           id,
			Contacts:     contacts,
			MsgSubject:   msgSubject,
			MsgBody:      msgBodyFileStringBuilder.String(),
			MsgBodyFile:  filename,
			GatewayType:  gatewaySelected.DBTable(),
			GatewayKey:   gatewaySelected.DBKey(),
			SendDateFrom: sendDate1,
			SendDateTo:   sendDate2,
			SendHours:    sendHours,
			Timezone:     timezoneSelected2,
			CreatedAt:    time.Now(),
		}
		err = dbutil.UpsertSaveable(db, b)
		if err != nil {
			return logAndReturnError(fmt.Errorf("cannot save broadcast: %s", err))
		}
		refreshChan <- struct{}{}
		return nil
	})
}

func generateMessageRandomContact(contacts []broadcast.Contact, msgTmpl *template.Template) (string, error) {
	if msgTmpl == nil {
		return "", nil
	}
	var buf strings.Builder
	c := contacts[rand.Intn(len(contacts))]
	err := msgTmpl.Execute(&buf, c.Keywords)
	if err != nil {
		return "", fmt.Errorf("msgTmpl.Execute failed: %s", err)
	}
	return buf.String(), nil
}
