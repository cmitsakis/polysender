package email

import (
	"fmt"
)

type Identity struct {
	Email   string
	Name    string
	SMTPKey []byte
}

func (i Identity) DBTable() string {
	return "gateway.email.identity"
}

func (i Identity) DBKey() []byte {
	return []byte(i.Email)
}

func (i Identity) String() string {
	return fmt.Sprintf("%s <%s>", i.Name, i.Email)
}
