package dbutil

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"

	"github.com/fxamacker/cbor/v2"
	bolt "go.etcd.io/bbolt"
)

var ErrNotFound = errors.New("not found")

func GetByTableKey(db *bolt.DB, table string, key []byte, pointer interface{}) error {
	if err := db.View(func(tx *bolt.Tx) error {
		return GetByTableKeyTx(tx, table, key, pointer)
	}); err != nil {
		return fmt.Errorf("transaction (View) failed: %w", err)
	}
	return nil
}

func GetByTableKeyTx(tx *bolt.Tx, table string, key []byte, pointer interface{}) error {
	b := tx.Bucket([]byte(table))
	if b == nil {
		return ErrNotFound
	}
	// LoggerDebug.Printf("[dbutil.GetByTableKeyTx] bucket %s Get(%x)\n", table, key)
	v := b.Get(key)
	if v == nil {
		return ErrNotFound
	}
	err := cbor.Unmarshal(v, pointer)
	if err != nil {
		return fmt.Errorf("cbor.Unmarshal failed: %w", err)
	}
	return nil
}

func GetByKey(db *bolt.DB, key []byte, pointer Saveable) error {
	if err := db.View(func(tx *bolt.Tx) error {
		return GetByKeyTx(tx, key, pointer)
	}); err != nil {
		return fmt.Errorf("transaction (View) failed: %w", err)
	}
	return nil
}

func GetByKeyTx(tx *bolt.Tx, key []byte, pointer Saveable) error {
	b := tx.Bucket([]byte(pointer.DBTable()))
	if b == nil {
		return ErrNotFound
	}
	// LoggerDebug.Printf("[dbutil.GetByKeyTx] bucket %s Get(%x) type: %T\n", pointer.DBTable(), key, pointer)
	v := b.Get(key)
	if v == nil {
		return ErrNotFound
	}
	err := cbor.Unmarshal(v, pointer)
	if err != nil {
		return fmt.Errorf("cbor.Unmarshal failed: %w", err)
	}
	return nil
}

type KeyPointer struct {
	Key     []byte
	Pointer Saveable
}

func GetMulti(db *bolt.DB, kps ...KeyPointer) error {
	if err := db.View(func(tx *bolt.Tx) error {
		return GetMultiTx(tx, kps...)
	}); err != nil {
		return fmt.Errorf("transaction (View) failed: %w", err)
	}
	return nil
}

func GetMultiTx(tx *bolt.Tx, kps ...KeyPointer) error {
	for _, kp := range kps {
		b := tx.Bucket([]byte(kp.Pointer.DBTable()))
		if b == nil {
			continue
		}
		// LoggerDebug.Printf("[dbutil.GetMultiTx] Get(%s)\n", kp.Key)
		itemValue := b.Get(kp.Key)
		if itemValue == nil {
			continue
		}
		err := cbor.Unmarshal(itemValue, kp.Pointer)
		if err != nil {
			return fmt.Errorf("cbor.Unmarshal failed: %w", err)
		}
	}
	return nil
}

func ForEach(db *bolt.DB, val Saveable, f func([]byte, interface{}) error) error {
	if err := db.View(func(tx *bolt.Tx) error {
		return ForEachTx(tx, val, f)
	}); err != nil {
		return fmt.Errorf("transaction (View) failed: %w", err)
	}
	return nil
}

func ForEachTx(tx *bolt.Tx, typ Saveable, f func([]byte, interface{}) error) error {
	// LoggerDebug.Printf("[dbutil.ForEachTx] searching for keys in bucket: %s\n", typ.DBTable())
	b := tx.Bucket([]byte(typ.DBTable()))
	if b == nil {
		return nil
	}
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		// LoggerDebug.Printf("[dbutil.ForEachTx] found key: %x\n", k)
		err := unmarshalAndRun(typ, k, v, f)
		if err != nil {
			return fmt.Errorf("unmarshalAndRun() failed: %w", err)
		}
	}
	return nil
}

func ForEachReverse(db *bolt.DB, val Saveable, f func([]byte, interface{}) error) error {
	if err := db.View(func(tx *bolt.Tx) error {
		return ForEachReverseTx(tx, val, f)
	}); err != nil {
		return fmt.Errorf("transaction (View) failed: %w", err)
	}
	return nil
}

func ForEachReverseTx(tx *bolt.Tx, typ Saveable, f func([]byte, interface{}) error) error {
	// LoggerDebug.Printf("[dbutil.ForEachReverseTx] searching for keys in bucket: %s\n", typ.DBTable())
	b := tx.Bucket([]byte(typ.DBTable()))
	if b == nil {
		return nil
	}
	c := b.Cursor()
	for k, v := c.Last(); k != nil; k, v = c.Prev() {
		// LoggerDebug.Printf("[dbutil.ForEachReverseTx] found key: %x\n", k)
		err := unmarshalAndRun(typ, k, v, f)
		if err != nil {
			return fmt.Errorf("unmarshalAndRun() failed: %w", err)
		}
	}
	return nil
}

func ForEachPrefix(db *bolt.DB, val Saveable, f func([]byte, interface{}) error) error {
	if err := db.View(func(tx *bolt.Tx) error {
		return ForEachTx(tx, val, f)
	}); err != nil {
		return fmt.Errorf("transaction (View) failed: %w", err)
	}
	return nil
}

func ForEachPrefixTx(tx *bolt.Tx, typ Saveable, prefix []byte, f func([]byte, interface{}) error) error {
	b := tx.Bucket([]byte(typ.DBTable()))
	if b == nil {
		return nil
	}
	c := b.Cursor()
	for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
		err := unmarshalAndRun(typ, k, v, f)
		if err != nil {
			return fmt.Errorf("unmarshalAndRun() failed: %w", err)
		}
	}
	return nil
}

func ForEachStartPrefix(db *bolt.DB, table string, start, prefix []byte, val interface{}, f func([]byte, interface{}) error) error {
	if err := db.View(func(tx *bolt.Tx) error {
		return ForEachStartPrefixTx(tx, table, start, prefix, val, f)
	}); err != nil {
		return fmt.Errorf("transaction (View) failed: %w", err)
	}
	return nil
}

func ForEachStartPrefixTx(tx *bolt.Tx, table string, start, prefix []byte, val interface{}, f func([]byte, interface{}) error) error {
	b := tx.Bucket([]byte(table))
	if b == nil {
		return nil
	}
	c := b.Cursor()
	for k, v := c.Seek(start); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
		// LoggerDebug.Printf("[dbutil.ForEachStartPrefixTx] found key: %x\n", k)
		err := unmarshalAndRun(val, k, v, f)
		if err != nil {
			return fmt.Errorf("unmarshalAndRun() failed: %w", err)
		}
	}
	return nil
}

func unmarshalAndRun(val interface{}, k, v []byte, f func([]byte, interface{}) error) error {
	valReflect := reflect.New(reflect.TypeOf(val).Elem())
	valPtr := valReflect.Interface()
	err := cbor.Unmarshal(v, valPtr)
	if err != nil {
		return fmt.Errorf("cbor.Unmarshal failed: %w", err)
	}
	err = f(k, valReflect.Elem().Interface())
	if err != nil {
		return fmt.Errorf("f failed: %w", err)
	}
	return nil
}
