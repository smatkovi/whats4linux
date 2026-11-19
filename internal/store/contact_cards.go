package store

import (
	"database/sql"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/lugvitc/whats4linux/internal/query"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ContactCard is one shared contact: its display name and vCard.
type ContactCard struct {
	DisplayName string `json:"displayName"`
	VCard       string `json:"vcard"`
}

// contactCardsFromMessage returns the vCards carried by a contact or
// contacts-array message (nil for any other message).
func contactCardsFromMessage(msg *waE2E.Message) []ContactCard {
	if msg == nil {
		return nil
	}
	if c := msg.GetContactMessage(); c != nil {
		return []ContactCard{{DisplayName: c.GetDisplayName(), VCard: c.GetVcard()}}
	}
	if arr := msg.GetContactsArrayMessage(); arr != nil {
		cards := make([]ContactCard, 0, len(arr.GetContacts()))
		for _, c := range arr.GetContacts() {
			cards = append(cards, ContactCard{DisplayName: c.GetDisplayName(), VCard: c.GetVcard()})
		}
		return cards
	}
	return nil
}

func insertContactCards(tx *sql.Tx, messageID string, cards []ContactCard) error {
	if len(cards) == 0 {
		return nil
	}
	if _, err := tx.Exec(query.DeleteContactCardsByMessageID, messageID); err != nil {
		return err
	}
	for i, c := range cards {
		if _, err := tx.Exec(query.InsertContactCard, messageID, i, c.DisplayName, c.VCard); err != nil {
			return err
		}
	}
	return nil
}

// summaryCardRE matches the HTML summary card that contactCard() renders into
// the message text: name in <b>, optional first phone number after <br>.
var summaryCardRE = regexp.MustCompile(`<div class="msg-card">👤 <b>(.*?)</b>(?:<br>(.*?))?</div>`)

// GetContactCards returns the shared contacts of a message together with its
// timestamp. Messages stored before vCards were kept fall back to a minimal
// vCard rebuilt from the summary card (name and first phone number only);
// reconstructed reports whether that happened.
func (ms *MessageStore) GetContactCards(messageID string) (cards []ContactCard, ts time.Time, reconstructed bool, err error) {
	var text sql.NullString
	var unix int64
	if err = ms.db.QueryRow(query.SelectMessageTextByID, messageID).Scan(&text, &unix); err != nil {
		if err == sql.ErrNoRows {
			err = fmt.Errorf("message not found")
		}
		return nil, time.Time{}, false, err
	}
	ts = time.Unix(unix, 0)

	rows, err := ms.db.Query(query.SelectContactCardsByMessageID, messageID)
	if err != nil {
		return nil, ts, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, vcard sql.NullString
		if err := rows.Scan(&name, &vcard); err != nil {
			return nil, ts, false, err
		}
		cards = append(cards, ContactCard{DisplayName: name.String, VCard: vcard.String})
	}
	if err := rows.Err(); err != nil {
		return nil, ts, false, err
	}
	if len(cards) > 0 {
		return cards, ts, false, nil
	}

	for _, m := range summaryCardRE.FindAllStringSubmatch(text.String, -1) {
		name := html.UnescapeString(m[1])
		phone := html.UnescapeString(m[2])
		cards = append(cards, ContactCard{DisplayName: name, VCard: MinimalVCard(name, phone)})
	}
	if len(cards) == 0 {
		return nil, ts, false, fmt.Errorf("this message has no contact")
	}
	return cards, ts, true, nil
}

// MinimalVCard builds a vCard 3.0 with a name and an optional phone number.
func MinimalVCard(name, phone string) string {
	escape := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, ",", `\,`)
		s = strings.ReplaceAll(s, ";", `\;`)
		return strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", `\n`)
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
	b.WriteString("N:" + escape(name) + ";;;;\r\n")
	b.WriteString("FN:" + escape(name) + "\r\n")
	if phone = strings.TrimSpace(phone); phone != "" {
		b.WriteString("TEL;TYPE=CELL:" + escape(phone) + "\r\n")
	}
	b.WriteString("END:VCARD\r\n")
	return b.String()
}

// DeleteContactCardsForTest removes stored vCards of a message, simulating a
// row written before they were kept. Only used by tests.
func (ms *MessageStore) DeleteContactCardsForTest(messageID string) error {
	_, err := ms.db.Exec(query.DeleteContactCardsByMessageID, messageID)
	return err
}
