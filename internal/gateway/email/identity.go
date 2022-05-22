package email

import (
	"fmt"

	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/gateway"
)

type GatewayCreator struct{}

func init() {
	gateway.Register(GatewayCreator{})
}

func (c GatewayCreator) DBTable() string {
	return "gateway.email.identity"
}

func (c GatewayCreator) NewGatewayFromDBKey(tx *bolt.Tx, key []byte) (gateway.Gateway, error) {
	var id Identity
	err := dbutil.GetByKeyTx(tx, key, &id)
	if err != nil { // don't ignore dbutil.ErrNotFound
		return nil, fmt.Errorf("failed to read email identity from database: %w", err)
	}
	err = dbutil.GetByKeyTx(tx, id.SMTPAccountKey, &id.SMTPAccount)
	if err != nil { // don't ignore dbutil.ErrNotFound
		return nil, fmt.Errorf("failed to read SMTP Key from database: %s", err)
	}
	return &id, nil
}

func (c GatewayCreator) NewGatewayEmpty() gateway.Gateway {
	var id Identity
	return &id
}

type Identity struct {
	Email          string
	Name           string
	SMTPAccountKey []byte      `cbor:"SMTPKey"`
	SMTPAccount    SMTPAccount `cbor:"-"`
}

var _ gateway.Gateway = (*Identity)(nil)

func (i Identity) DBTable() string {
	return "gateway.email.identity"
}

func (i Identity) DBKey() []byte {
	return []byte(i.Email)
}

func (i Identity) GetLimitPerMinute() int {
	return i.SMTPAccount.LimitPerMinute
}

func (i Identity) GetLimitPerHour() int {
	return i.SMTPAccount.LimitPerHour
}

func (i Identity) GetLimitPerDay() int {
	return i.SMTPAccount.LimitPerDay
}

func (i Identity) GetConcurrencyMax() int {
	if i.SMTPAccount.ConcurrencyMax > 0 {
		return i.SMTPAccount.ConcurrencyMax
	} else {
		return 1
	}
}

func (i Identity) String() string {
	return fmt.Sprintf("%s <%s>", i.Name, i.Email)
}
