package email

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/oklog/ulid/v2"
	bolt "go.etcd.io/bbolt"

	"go.polysender.org/internal/dbutil"
	"go.polysender.org/internal/gateway"
	"go.polysender.org/internal/workerpool"
)

type SMTPAccount struct {
	ID                        ulid.ULID
	Host                      string
	Port                      int
	Username                  string
	Password                  string
	AuthType                  string
	ConnectionEncryption      string
	TLSInsecureSkipVerify     bool
	HELOHost                  string
	LimitPerMinute            int
	LimitPerHour              int
	LimitPerDay               int
	ConcurrencyMax            int
	ConnectionReuseCountLimit int
}

var _ gateway.Gateway = (*SMTPAccount)(nil)

func (s SMTPAccount) DBTable() string {
	return "gateway.email.smtp"
}

func (s SMTPAccount) DBKey() []byte {
	return s.ID[:]
}

func (s SMTPAccount) String() string {
	return fmt.Sprintf("ID: %s, Host: %v, Port: %v, Username: %v", s.ID, s.Host, s.Port, s.Username)
}

func (s SMTPAccount) GetLimitPerMinute() int {
	return s.LimitPerMinute
}

func (s SMTPAccount) GetLimitPerHour() int {
	return s.LimitPerHour
}

func (s SMTPAccount) GetLimitPerDay() int {
	return s.LimitPerDay
}

func (s SMTPAccount) GetConcurrencyMax() int {
	if s.ConcurrencyMax > 0 {
		return s.ConcurrencyMax
	} else {
		return 1
	}
}

func NewSMTPAccountFromKey(tx *bolt.Tx, key []byte) (*SMTPAccount, error) {
	var acc SMTPAccount
	var id Identity
	err := dbutil.GetByKeyTx(tx, key, &id)
	if err != nil { // don't ignore dbutil.ErrNotFound
		return nil, fmt.Errorf("failed to read email identity from database: %w", err)
	}
	err = dbutil.GetByKeyTx(tx, id.SMTPKey, &acc)
	if err != nil { // don't ignore dbutil.ErrNotFound
		return nil, fmt.Errorf("failed to read SMTP Key from database: %s", err)
	}
	return &acc, nil
}

type SenderClientSMTP struct {
	SMTPAccount            SMTPAccount
	From                   Identity
	saslClient             sasl.Client
	TLSConfig              *tls.Config
	conn                   *smtp.Client
	connectionReuseCounter int
	connectionReuseStarted time.Time
	ListUnsubscribeEnabled bool
	ListUnsubscribeEmail   string
}

var _ gateway.SenderClient = (*SenderClientSMTP)(nil)

func NewSenderClientFromKey(db *bolt.DB, key []byte) (*SenderClientSMTP, error) {
	var acc SMTPAccount
	var id Identity
	var settingListUnsubscribeEnabled SettingListUnsubscribeEnabled
	var listUnsubscribeEmail string
	// read from database: id, acc, settingListUnsubscribeEnabled, listUnsubscribeEmail
	if err := db.View(func(tx *bolt.Tx) error {
		err := dbutil.GetByKeyTx(tx, key, &id)
		if err != nil { // don't ignore dbutil.ErrNotFound
			return fmt.Errorf("failed to read email identity from database: %w", err)
		}
		err = dbutil.GetByKeyTx(tx, id.SMTPKey, &acc)
		if err != nil { // don't ignore dbutil.ErrNotFound
			return fmt.Errorf("failed to read SMTP Key from database: %s", err)
		}
		err = dbutil.GetByKeyTx(tx, settingListUnsubscribeEnabled.DBKey(), &settingListUnsubscribeEnabled)
		if err != nil && !errors.Is(err, dbutil.ErrNotFound) {
			return fmt.Errorf("failed to read setting ListUnsubscribeEnabled from database: %s", err)
		}
		if settingListUnsubscribeEnabled {
			var listUnsubscribeEmailIdentity Identity
			err = DBGetSettingListUnsubscribeEmailIdentity(tx, &listUnsubscribeEmailIdentity)
			if err != nil {
				return err
			}
			listUnsubscribeEmail = listUnsubscribeEmailIdentity.Email
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &SenderClientSMTP{
		SMTPAccount:            acc,
		From:                   id,
		ListUnsubscribeEnabled: bool(settingListUnsubscribeEnabled),
		ListUnsubscribeEmail:   listUnsubscribeEmail,
	}, nil
}

func (c *SenderClientSMTP) PreSend(ctx context.Context) error {
	var err error
	_ = c.PostSend(ctx)
	c.connectionReuseCounter = 0
	c.connectionReuseStarted = time.Now()
	switch c.SMTPAccount.AuthType {
	case "PLAIN", "":
		c.saslClient = sasl.NewPlainClient("", c.SMTPAccount.Username, c.SMTPAccount.Password)
	case "NONE":
	default:
		return fmt.Errorf("unknown auth type %s", c.SMTPAccount.AuthType)
	}
	c.TLSConfig = &tls.Config{
		InsecureSkipVerify: c.SMTPAccount.TLSInsecureSkipVerify,
		ServerName:         c.SMTPAccount.Host,
	}
	switch c.SMTPAccount.ConnectionEncryption {
	case "STARTTLS":
		port := 587
		if c.SMTPAccount.Port != 0 {
			port = c.SMTPAccount.Port
		}
		c.conn, err = smtp.Dial(fmt.Sprintf("%s:%d", c.SMTPAccount.Host, port))
		if err != nil {
			return fmt.Errorf("smtp.Dial failed: %s", err)
		}
		starttlsSupported, _ := c.conn.Extension("STARTTLS")
		if !starttlsSupported {
			_ = c.PostSend(ctx)
			return fmt.Errorf("SMTP server does not support STARTTLS")
		}
		err = c.conn.StartTLS(c.TLSConfig)
		if err != nil {
			_ = c.PostSend(ctx)
			return fmt.Errorf("conn.StartTLS failed: %s", err)
		}
	case "TLS":
		port := 465
		if c.SMTPAccount.Port != 0 {
			port = c.SMTPAccount.Port
		}
		c.conn, err = smtp.DialTLS(fmt.Sprintf("%s:%d", c.SMTPAccount.Host, port), c.TLSConfig)
		if err != nil {
			return fmt.Errorf("smtp.DialTLS failed: %s", err)
		}
	case "INSECURE":
		port := 587
		if c.SMTPAccount.Port != 0 {
			port = c.SMTPAccount.Port
		}
		c.conn, err = smtp.Dial(fmt.Sprintf("%s:%d", c.SMTPAccount.Host, port))
		if err != nil {
			return fmt.Errorf("smtp.Dial failed: %s", err)
		}
	default:
		return fmt.Errorf("invalid connection encryption value: %v", c.SMTPAccount.ConnectionEncryption)
	}
	if c.SMTPAccount.HELOHost != "" {
		err = c.conn.Hello(c.SMTPAccount.HELOHost)
		if err != nil {
			_ = c.PostSend(ctx)
			return fmt.Errorf("SMTP HELO failed: %s", err)
		}
	}
	err = c.conn.Noop()
	if err != nil {
		_ = c.PostSend(ctx)
		return fmt.Errorf("SMTP Noop failed: %s", err)
	}
	if c.SMTPAccount.AuthType == "NONE" {
		return nil
	}
	err = c.conn.Auth(c.saslClient)
	if err != nil {
		_ = c.PostSend(ctx)
		var errSMTP *smtp.SMTPError
		if errors.As(err, &errSMTP) {
			return fmt.Errorf("SMTP Auth failed with code %d: %v", errSMTP.Code, errSMTP)
		}
		return fmt.Errorf("SMTP Auth failed: %s", err)
	}
	return nil
}

func (c *SenderClientSMTP) PostSend(ctx context.Context) error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Quit()
	if err != nil {
		err := c.conn.Close()
		if err != nil {
			return fmt.Errorf("SMTP conn.Close() failed: %s", err)
		}
	}
	c.conn = nil
	return nil
}

func (c *SenderClientSMTP) Send(ctx context.Context, to string, subject, msg, broadcastID string) error {
	if c.SMTPAccount.ConnectionReuseCountLimit < 2 ||
		c.connectionReuseCounter >= c.SMTPAccount.ConnectionReuseCountLimit ||
		time.Since(c.connectionReuseStarted) >= 300*time.Second { // 300s is postfix's default value for smtp_connection_reuse_time_limit
		// reconnect
		err := c.PreSend(ctx)
		if err != nil {
			return workerpool.ErrorWrapRetryable(fmt.Errorf("failed to connect to server: %s", err))
		}
	}
	c.connectionReuseCounter++
	fromParsed, err := mail.ParseAddress(c.From.String())
	if err != nil {
		return fmt.Errorf("failed to parse sender address %s: %s", c.From.String(), err)
	}
	toParsed, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("failed to parse recipient address %s: %s", to, err)
	}

	err = c.conn.Noop()
	if err != nil {
		return workerpool.ErrorWrapRetryable(fmt.Errorf("SMTP Noop failed: %s", err))
	}
	err = c.conn.Mail(fromParsed.Address, nil)
	if err != nil {
		var errSMTP *smtp.SMTPError
		if errors.As(err, &errSMTP) {
			return workerpool.ErrorWrapRetryable(fmt.Errorf("SMTP Mail failed with code %d: %v", errSMTP.Code, errSMTP))
		}
		return workerpool.ErrorWrapRetryable(fmt.Errorf("SMTP Mail failed: %s", err))
	}
	err = c.conn.Rcpt(to)
	if err != nil {
		var errSMTP *smtp.SMTPError
		if errors.As(err, &errSMTP) {
			return workerpool.ErrorWrapRetryable(fmt.Errorf("SMTP Rcpt failed with code %d: %v", errSMTP.Code, errSMTP))
		}
		return workerpool.ErrorWrapRetryable(fmt.Errorf("SMTP Rcpt failed: %s", err))
	}

	var header mail.Header
	header.SetContentType("text/plain", map[string]string{"charset": "UTF-8"})
	header.SetDate(time.Now().UTC())
	header.SetAddressList("From", []*mail.Address{fromParsed})
	header.SetAddressList("To", []*mail.Address{toParsed})
	// header.GenerateMessageID()
	header.SetMessageID(generateMessageID(broadcastID, to, c.SMTPAccount.Host))
	header.SetSubject(subject)
	if c.ListUnsubscribeEnabled {
		var listUnsubscribeEmail string
		if c.ListUnsubscribeEmail != "" {
			listUnsubscribeEmail = c.ListUnsubscribeEmail
		} else {
			listUnsubscribeEmail = c.From.Email
		}
		header.Set("List-Unsubscribe", fmt.Sprintf("<mailto:%s?subject=unsubscribe>", listUnsubscribeEmail))
	}

	dataWriter, err := c.conn.Data()
	if err != nil {
		var errSMTP *smtp.SMTPError
		if errors.As(err, &errSMTP) {
			return workerpool.ErrorWrapRetryable(fmt.Errorf("SMTP Data failed with code %d: %v", errSMTP.Code, errSMTP))
		}
		return workerpool.ErrorWrapRetryable(fmt.Errorf("SMTP Data failed: %s", err))
	}
	defer dataWriter.Close()

	dataBodyWriter, err := mail.CreateSingleInlineWriter(dataWriter, header)
	if err != nil {
		return workerpool.ErrorWrapRetryable(fmt.Errorf("mail.CreateSingleInlineWriter() failed: %s", err))
	}
	defer dataBodyWriter.Close()

	_, err = io.Copy(dataBodyWriter, strings.NewReader(msg))
	if err != nil {
		return workerpool.ErrorWrapRetryable(fmt.Errorf("io.Copy() failed: %s", err))
	}

	return nil
}

func generateMessageID(broadcastID, to, domain string) string {
	h := sha256.New()
	h.Write([]byte(broadcastID))
	h.Write([]byte(to))
	id := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h.Sum(nil)[:16])
	return fmt.Sprintf("%s@%s", id, domain)
}
