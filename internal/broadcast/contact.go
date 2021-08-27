package broadcast

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
)

type Contact struct {
	Recipient string
	Keywords  map[string]string
}

func ReadContactsFromReader(f io.Reader) ([]Contact, error) {
	scanner := bufio.NewScanner(f)
	contacts := make([]Contact, 0)
	for scanner.Scan() {
		var c Contact
		// TODO: validate & normalize recipient, but we don't know if it's email or phone number
		c.Recipient = scanner.Text()
		contacts = append(contacts, c)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner failed: %w", err)
	}
	return contacts, nil
}

func ReadContactsFromReaderCSV(f io.Reader, delimiter rune, hasHeader bool, recipientColumnStr string, recipientColumnInt int) ([]Contact, error) {
	if f == nil {
		return nil, fmt.Errorf("f == nil")
	}
	r := csv.NewReader(f)
	if delimiter != 0 {
		r.Comma = delimiter
	}
	// records, err := r.ReadAll()
	headers := make([]string, 0)
	contacts := make([]Contact, 0)
	seenNumbers := make(map[string]struct{})
	i := -1
	for {
		i++
		row, err := r.Read()
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return contacts, err
		}
		if i == 0 && hasHeader {
			headers = append(headers, row...)
			continue
		}

		var c Contact
		if len(row) > 0 && hasHeader {
			c.Keywords = make(map[string]string)
			for j, field := range row {
				c.Keywords[headers[j]] = field
			}
		}
		if hasHeader && recipientColumnStr != "" {
			var exists bool
			c.Recipient, exists = c.Keywords[recipientColumnStr]
			if !exists {
				return nil, fmt.Errorf("recipient column (%s) not found", recipientColumnStr)
			}
		} else if recipientColumnInt != 0 {
			if recipientColumnInt < len(row) {
				c.Recipient = row[recipientColumnInt]
			} else {
				return nil, fmt.Errorf("recepient column (%d) greater than the number of columns (%d)", recipientColumnInt, len(row))
			}
		} else {
			return nil, fmt.Errorf("recipient column not set")
		}
		if _, exists := seenNumbers[c.Recipient]; exists {
			continue
		}
		seenNumbers[c.Recipient] = struct{}{}
		contacts = append(contacts, c)
	}
}
