package dbutil

type Saveable interface {
	DBTable() string
	DBKey() []byte
}
