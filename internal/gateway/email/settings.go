package email

import (
	"errors"
	"fmt"

	"go.polysender.org/internal/dbutil"

	bolt "go.etcd.io/bbolt"
)

type SettingListUnsubscribeEnabled bool

func (s SettingListUnsubscribeEnabled) DBTable() string {
	return "settings"
}

func (s SettingListUnsubscribeEnabled) DBKey() []byte {
	return []byte("gateway.email.list_unsubscribe_enabled")
}

type SettingListUnsubscribeEmailKey []byte

func (s SettingListUnsubscribeEmailKey) DBTable() string {
	return "settings"
}

func (s SettingListUnsubscribeEmailKey) DBKey() []byte {
	return []byte("gateway.email.list_unsubscribe_email_key")
}

func DBGetSettingListUnsubscribeEmailIdentity(tx *bolt.Tx, listUnsubscribeEmailIdentity *Identity) error {
	var listUnsubscribeEmailKey SettingListUnsubscribeEmailKey
	err := dbutil.GetByKeyTx(tx, listUnsubscribeEmailKey.DBKey(), &listUnsubscribeEmailKey)
	if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
		return fmt.Errorf("failed to read setting ListUnsubscribeEmail from database: %w", err)
	}
	if errors.Is(err, dbutil.ErrNotFound) {
		return nil
	}

	err = dbutil.GetByKeyTx(tx, listUnsubscribeEmailKey, listUnsubscribeEmailIdentity)
	if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
		return fmt.Errorf("failed to read email identity from database: %w", err)
	}

	return nil
}

type SettingListUnsubscribeHeader string

func (s SettingListUnsubscribeHeader) DBTable() string {
	return "settings"
}

func (s SettingListUnsubscribeHeader) DBKey() []byte {
	return []byte("gateway.email.list_unsubscribe_header")
}
