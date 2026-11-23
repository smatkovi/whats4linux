package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/nyaruka/phonenumbers"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type Contact struct {
	Phno       string `json:"phno"`
	JID        string `json:"jid"`
	Short      string `json:"short"`
	FullName   string `json:"full_name"`
	PushName   string `json:"push_name"`
	IsBusiness bool   `json:"is_business"`
	AvatarURL  string `json:"avatar_url"`
}

func canonicalUserJID(ctx context.Context, client *whatsmeow.Client, jid types.JID) types.JID {
	if jid.ActualAgent() == types.LIDDomain {
		if pn, err := client.Store.LIDs.GetPNForLID(ctx, jid); err == nil {
			jid = pn
		}
	}
	return jid.ToNonAD()
}

func (a *Api) GetContact(jid types.JID) (*Contact, error) {
	jid = canonicalUserJID(a.ctx, a.waClient, jid)
	contact, err := a.waClient.Store.Contacts.GetContact(a.ctx, jid)
	if err != nil {
		return nil, err
	}
	rawNum := "+" + jid.User
	// Parse phone number to use as International Format
	num, err := phonenumbers.Parse(rawNum, "")
	if err != nil {
		return nil, fmt.Errorf("invalid phone number")
	}

	return &Contact{
		Phno:       phonenumbers.Format(num, phonenumbers.INTERNATIONAL),
		JID:        jid.String(),
		FullName:   contact.FullName,
		Short:      contact.FirstName,
		PushName:   contact.PushName,
		IsBusiness: contact.BusinessName != "",
	}, nil
}

func (a *Api) FetchContacts() ([]Contact, error) {
	rawContacts, err := a.waClient.Store.Contacts.GetAllContacts(a.ctx)
	if err != nil {
		return nil, err
	}
	contacts := make([]Contact, 0, len(rawContacts))
	for jid, c := range rawContacts {
		// The store also holds LID-keyed entries (group members etc.); their
		// "number" is an opaque ID, not something to show or message.
		if jid.Server != types.DefaultUserServer {
			continue
		}
		contacts = append(contacts, Contact{
			Phno:       formatPhone(jid.User),
			JID:        jid.String(),
			FullName:   c.FullName,
			Short:      c.FirstName,
			PushName:   c.PushName,
			IsBusiness: c.BusinessName != "",
		})
	}
	return contacts, nil
}

// formatPhone renders a JID user part as an international phone number,
// falling back to "+digits" when libphonenumber can't parse it.
func formatPhone(user string) string {
	raw := "+" + user
	num, err := phonenumbers.Parse(raw, "")
	if err != nil {
		return raw
	}
	return phonenumbers.Format(num, phonenumbers.INTERNATIONAL)
}

// internationalDigits turns user input like "+43 660 1234567" or
// "0043 660 1234567" into bare international digits, rejecting national
// numbers ("0660 …") that lack a country code.
func internationalDigits(phone string) (string, error) {
	trimmed := strings.TrimSpace(phone)
	digits := normalizePairingPhone(trimmed)
	international := strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "00")
	if strings.HasPrefix(digits, "0") || (!international && len(digits) < 10) {
		return "", fmt.Errorf("enter the number with country code, e.g. +43 660 1234567")
	}
	if len(digits) < 7 || len(digits) > 15 {
		return "", fmt.Errorf("%q is not a valid phone number", trimmed)
	}
	return digits, nil
}

// ResolvePhoneChat checks that a phone number is on WhatsApp and returns the
// contact to start a chat with (phone-number JID plus the best known name).
func (a *Api) ResolvePhoneChat(phone string) (Contact, error) {
	if a.waClient == nil || a.waClient.Store == nil || a.waClient.Store.ID == nil {
		return Contact{}, fmt.Errorf("not logged in")
	}
	digits, err := internationalDigits(phone)
	if err != nil {
		return Contact{}, err
	}

	resp, err := a.waClient.IsOnWhatsApp(a.ctx, []string{"+" + digits})
	if err != nil {
		return Contact{}, fmt.Errorf("could not check the number: %w", err)
	}
	if len(resp) == 0 || !resp[0].IsIn {
		return Contact{}, fmt.Errorf("%s is not on WhatsApp", formatPhone(digits))
	}

	// The app keys one-to-one chats by phone-number JID; the response's main
	// JID is often the LID.
	jid := resp[0].PhoneNumber
	if jid.IsEmpty() || jid.Server != types.DefaultUserServer {
		jid = resp[0].JID
	}
	if jid.IsEmpty() || jid.Server != types.DefaultUserServer {
		jid = types.NewJID(digits, types.DefaultUserServer)
	}
	jid = jid.ToNonAD()

	c := Contact{JID: jid.String(), Phno: formatPhone(jid.User)}
	if info, err := a.waClient.Store.Contacts.GetContact(a.ctx, jid); err == nil && info.Found {
		c.FullName = info.FullName
		c.Short = info.FirstName
		c.PushName = info.PushName
		c.IsBusiness = info.BusinessName != ""
	}
	if vn := resp[0].VerifiedName; vn != nil && vn.Details != nil {
		c.IsBusiness = true
		if c.FullName == "" {
			c.FullName = vn.Details.GetVerifiedName()
		}
	}
	return c, nil
}
