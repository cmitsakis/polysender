package dbutil

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	bolt "go.etcd.io/bbolt"
)

var ErrKeyExists = errors.New("key already exists")

func UpsertTableKeyValue(db *bolt.DB, table string, key []byte, val interface{}) error {
	if err := db.Update(func(tx *bolt.Tx) error {
		return UpsertTableKeyValueTx(tx, table, key, val)
	}); err != nil {
		return fmt.Errorf("transaction (Update) failed: %w", err)
	}
	return nil
}

func UpsertTableKeyValueTx(tx *bolt.Tx, table string, key []byte, val interface{}) error {
	b, err := tx.CreateBucketIfNotExists([]byte(table))
	if err != nil {
		return fmt.Errorf("tx.CreateBucketIfNotExists failed: %w", err)
	}
	valEnc, err := cbor.Marshal(val)
	if err != nil {
		return fmt.Errorf("cbor.Marshal failed: %w", err)
	}
	// LoggerDebug.Printf("[dbutil.UpsertKeystringValueTx] key: %x value: %+v\n", key, val)
	err = b.Put(key, valEnc)
	if err != nil {
		return fmt.Errorf("cannot save item with key %x to database: %w", key, err)
	}
	return nil
}

func UpsertSaveable(db *bolt.DB, item Saveable) error {
	if err := db.Update(func(tx *bolt.Tx) error {
		return UpsertSaveableTx(tx, item)
	}); err != nil {
		return fmt.Errorf("transaction (Update) failed: %w", err)
	}
	return nil
}

func UpsertSaveableTx(tx *bolt.Tx, item Saveable) error {
	b, err := tx.CreateBucketIfNotExists([]byte(item.DBTable()))
	if err != nil {
		return fmt.Errorf("tx.CreateBucketIfNotExists failed: %w", err)
	}
	itemEnc, err := cbor.Marshal(item)
	if err != nil {
		return fmt.Errorf("cbor.Marshal failed: %s", err)
	}
	// LoggerDebug.Printf("[dbutil.UpsertSavealbleTx] key: %x value: %+v\n", item.DBKey(), item)
	err = b.Put(item.DBKey(), itemEnc)
	if err != nil {
		return fmt.Errorf("cannot save item with key %x to database: %w", item.DBKey(), err)
	}
	return nil
}

func InsertSaveable(db *bolt.DB, item Saveable) error {
	if err := db.Update(func(tx *bolt.Tx) error {
		return InsertSaveableTx(tx, item)
	}); err != nil {
		return fmt.Errorf("transaction (Update) failed: %w", err)
	}
	return nil
}

func InsertSaveableTx(tx *bolt.Tx, item Saveable) error {
	b, err := tx.CreateBucketIfNotExists([]byte(item.DBTable()))
	if err != nil {
		return fmt.Errorf("tx.CreateBucketIfNotExists failed: %w", err)
	}
	if v := b.Get(item.DBKey()); v != nil {
		return ErrKeyExists
	}
	itemEnc, err := cbor.Marshal(item)
	if err != nil {
		return fmt.Errorf("cbor.Marshal failed: %w", err)
	}
	// LoggerDebug.Printf("[dbutil.InsertSaveableTx] key: %x value: %+v\n", item.DBKey(), item)
	err = b.Put(item.DBKey(), itemEnc)
	if err != nil {
		return fmt.Errorf("cannot save item with key %x to database: %w", item.DBKey(), err)
	}
	return nil
}

func DeleteByTableKey(db *bolt.DB, table string, key []byte) error {
	if err := db.Update(func(tx *bolt.Tx) error {
		return DeleteByTableKeyTx(tx, table, key)
	}); err != nil {
		return fmt.Errorf("transaction (Update) failed: %w", err)
	}
	return nil
}

func DeleteByTableKeyTx(tx *bolt.Tx, table string, key []byte) error {
	// LoggerDebug.Printf("[dbutil.DeleteByTableKeyTx] deleting key %s\n", key)
	b := tx.Bucket([]byte(table))
	if b == nil {
		return nil
	}
	if err := b.Delete(key); err != nil {
		return fmt.Errorf("cannot delete item with key %x: %w", key, err)
	}
	return nil
}

func DeletePrefix(db *bolt.DB, table string, prefix []byte) error {
	if err := db.Update(func(tx *bolt.Tx) error {
		return DeleteByTableKeyTx(tx, table, prefix)
	}); err != nil {
		return fmt.Errorf("transaction (Update) failed: %w", err)
	}
	return nil
}

func DeletePrefixTx(tx *bolt.Tx, table string, prefix []byte) error {
	b := tx.Bucket([]byte(table))
	if b == nil {
		return nil
	}
	c := b.Cursor()
	for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Seek(prefix) {
		err := c.Delete()
		if err != nil {
			return fmt.Errorf("delete failed: %w", err)
		}
	}
	return nil
}
