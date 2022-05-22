package main

import (
	"fmt"
	"strings"

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

func tabEmailIdentities(w fyne.Window) *container.TabItem {
	refreshChan := make(chan struct{}, 1)
	newAccountBtn := widget.NewButtonWithIcon("New Identity", theme.ContentAddIcon(), func() {
		emailIdentityNewOrEdit(w, nil, func(idNew email.Identity) {
			err := dbutil.InsertSaveable(db, idNew)
			if err != nil {
				logAndShowError(fmt.Errorf("cannot write to database: %s", err), w)
			}
			refreshChan <- struct{}{}
		})
	})
	listPage := container2.NewList(
		w,
		refreshChan,
		[]widget2.Action{
			{
				Name: "Edit",
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						emailIdentityNewOrEdit(w, v.DBKey(), func(idNew email.Identity) {
							err := dbutil.UpsertSaveable(db, idNew)
							if err != nil {
								logAndShowError(fmt.Errorf("cannot write to database: %s", err), w)
							}
							refreshChan <- struct{}{}
						})
					}
				},
			}, {
				Name: "Delete",
				Func: func(v dbutil.Saveable, refreshChan chan<- struct{}) func() {
					return func() {
						content := widget.NewLabel("Are you sure you want to delete this email identity?")
						dialog.ShowCustomConfirm("Delete email identity", "Confirm", "Cancel", content, func(submit bool) {
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
		func(refreshChan <-chan struct{}, l *widget2.List, noticeLabel *widget.Label) {
			for range refreshChan {
				values := make([]dbutil.Saveable, 0)
				err := dbutil.ForEach(db, &email.Identity{}, func(k []byte, v interface{}) error {
					vCasted, ok := v.(email.Identity)
					if !ok {
						return fmt.Errorf("value %v is not an email identity", v)
					}
					values = append(values, vCasted)
					return nil
				})
				l.UpdateAndRefresh(values)
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
	content := container.NewBorder(newAccountBtn, nil, nil, nil, listPage)
	return container.NewTabItemWithIcon("Identities", theme.MailComposeIcon(), content)
}

func emailIdentityNewOrEdit(w fyne.Window, key []byte, next func(email.Identity)) {
	// retrieve all SMTP accounts
	smtpAccountsKeysULID := make([]string, 0)
	smtpAccountsKeysFriendlyNames := make([]string, 0)
	err := dbutil.ForEachReverse(db, &email.SMTPAccount{}, func(k []byte, v interface{}) error {
		vSMTPAccount := v.(email.SMTPAccount)
		smtpAccountsKeysULID = append(smtpAccountsKeysULID, vSMTPAccount.ID.String())
		smtpAccountsKeysFriendlyNames = append(smtpAccountsKeysFriendlyNames, fmt.Sprintf("%s Host: %s Username: %s", vSMTPAccount.ID.String(), vSMTPAccount.Host, vSMTPAccount.Username))
		return nil
	})
	if err != nil {
		logAndShowError(fmt.Errorf("cannot read from database: %s", err), w)
		return
	}

	// retrieve saved email identity
	var id email.Identity
	if key != nil {
		err = dbutil.GetByKey(db, key, &id)
		if err != nil { // don't ignore dbutil.ErrNotFound
			logAndShowError(fmt.Errorf("cannot read from database: %s", err), w)
			return
		}
	}
	var idSMTPAccountKeyString string
	if id.SMTPAccountKey != nil {
		var idSMTPAccountKeyULID ulid.ULID
		err = idSMTPAccountKeyULID.UnmarshalBinary(id.SMTPAccountKey)
		if err != nil {
			logAndShowError(fmt.Errorf("email identity has invalid SMTP key: %s", err), w)
			return
		}
		idSMTPAccountKeyString = idSMTPAccountKeyULID.String()
	}

	var smtpAccountDescription string
	if len(smtpAccountsKeysFriendlyNames) == 0 {
		smtpAccountDescription = "Create an SMTP account and connect it with this email identity in order to be able to send emails."
	} else {
		smtpAccountDescription = "Connect this email identity with an SMTP account in order to be able to send emails."
	}
	fields := []form.FormField{
		{
			Name:          "Name",
			ExistingValue: id.Name,
		},
		{
			Name:          "Email",
			ExistingValue: id.Email,
			ReadOnly:      key != nil,
		},
		{
			Name:          "SMTP Account",
			ExistingValue: idSMTPAccountKeyString,
			Type:          form.FormFieldTypeRadio,
			Options:       smtpAccountsKeysFriendlyNames,
			OptionsValues: smtpAccountsKeysULID,
			Description:   smtpAccountDescription,
		},
	}

	var description string
	if key != nil {
		description = fmt.Sprintf("Edit details of %s", id.Email)
	} else {
		description = "Enter your email identity details"
	}
	form.ShowFormPopup(w, "Email Identity Details", description, fields, func(inputValues []string) error {
		if len(inputValues) != 3 {
			panic("len(inputValues) != 3")
		}
		var eml string
		if key != nil {
			eml = id.Email
		} else {
			eml = inputValues[1]
		}
		var idNew email.Identity
		if inputValues[2] != "" {
			fields := strings.Fields(inputValues[2])
			if len(fields) == 0 {
				return logAndReturnError(fmt.Errorf("no fields found"))
			}
			idNewSMTPAccountKeyULID, err := ulid.ParseStrict(fields[0])
			if err != nil {
				return logAndReturnError(fmt.Errorf("failed to parse SMTP ULID: %s", err))
			}
			idNew = email.Identity{
				Name:           inputValues[0],
				Email:          eml,
				SMTPAccountKey: idNewSMTPAccountKeyULID[:],
			}
		} else {
			idNew = email.Identity{
				Name:  inputValues[0],
				Email: eml,
			}
		}
		next(idNew)
		return nil
	})
}
