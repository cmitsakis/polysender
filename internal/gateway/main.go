package gateway

import (
	"context"
	"fmt"
	"sort"

	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/dbutil"
)

type GatewayCreator interface {
	DBTable() string
	NewGatewayFromDBKey(tx *bolt.Tx, key []byte) (Gateway, error)
	NewGatewayEmpty() Gateway
}

var gatewayCreators = make(map[string]GatewayCreator)

// Each gateway package's init() function calls Register() in order to add a GatewayCreator to gatewayCreators.
// Similar to sql.Register().
// This approach enables the addition of new gateways without editing this package.
func Register(creator GatewayCreator) {
	table := creator.DBTable()
	if _, exists := gatewayCreators[table]; exists {
		panic(fmt.Sprintf("Cannot register gateway. Table name '%s' already exists", table))
	}
	gatewayCreators[table] = creator
}

func NewGatewayFromDBTableAndKey(tx *bolt.Tx, table string, key []byte) (Gateway, error) {
	creator, exists := gatewayCreators[table]
	if !exists {
		return nil, fmt.Errorf("gatewayCreator does not exist")
	}
	return creator.NewGatewayFromDBKey(tx, key)
}

func GetGateways(db *bolt.DB) ([]Gateway, error) {
	var gs []Gateway
	if err := db.View(func(tx *bolt.Tx) error {
		for _, creator := range gatewayCreators {
			err := dbutil.ForEachTx(tx, creator.NewGatewayEmpty(), func(k []byte, v interface{}) error {
				g := v.(Gateway)
				gs = append(gs, g)
				return nil
			})
			if err != nil {
				return fmt.Errorf("dbutil.ForEachTx failed: %s", err)
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("transaction failed: %s", err)
	}
	sort.Slice(gs, func(i, j int) bool {
		return gs[i].String() > gs[j].String()
	})
	return gs, nil
}

type Gateway interface {
	NewSenderClient(db *bolt.DB, workerID int) (SenderClient, error)
	GetLimitPerMinute() int
	GetLimitPerHour() int
	GetLimitPerDay() int
	GetConcurrencyMax() int
	String() string
	DBTable() string // Gateway should implement dbutil.Saveable
	DBKey() []byte
}

type SenderClient interface {
	PreSend(ctx context.Context) error
	Send(ctx context.Context, to string, subject, message, broadcastID string) error
	PostSend(ctx context.Context) error
}
