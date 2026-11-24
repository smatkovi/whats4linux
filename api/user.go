package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/lugvitc/whats4linux/internal/wa"
	"github.com/nyaruka/phonenumbers"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// GetMyJID returns the logged-in user's phone-number part, used to identify
// which reactions are our own (for toggling them off).
func (a *Api) GetMyJID() string {
	if a.waClient.Store.ID == nil {
		return ""
	}
	return a.waClient.Store.ID.User
}

func (a *Api) GetProfile(jidStr string) (Contact, error) {
	var targetJID types.JID
	if jidStr == "" {
		if a.waClient.Store.ID == nil {
			return Contact{}, fmt.Errorf("not logged in")
		}
		targetJID = *a.waClient.Store.ID
	} else {
		var err error
		targetJID, err = types.ParseJID(jidStr)
		if err != nil {
			return Contact{}, fmt.Errorf("invalid JID: %w", err)
		}
	}

	contact, _ := a.waClient.Store.Contacts.GetContact(a.ctx, targetJID.ToNonAD())
	rawNum := "+" + targetJID.User

	jid := rawNum
	num, err := phonenumbers.Parse(rawNum, "")
	if err == nil && phonenumbers.IsValidNumber(num) {
		jid = phonenumbers.Format(num, phonenumbers.INTERNATIONAL)
	}

	pic, _ := a.waClient.GetProfilePictureInfo(a.ctx, targetJID, &whatsmeow.GetProfilePictureParams{
		Preview: true,
	})
	var avatarURL string
	if pic != nil {
		avatarURL = pic.URL
	}

	pushName := contact.PushName
	// If it's self, try to get pushname from store if contact pushname is empty
	if jidStr == "" && a.waClient.Store.PushName != "" {
		pushName = a.waClient.Store.PushName
	}

	return Contact{
		Phno:       jid,
		JID:        targetJID.String(),
		FullName:   contact.FullName,
		Short:      contact.FirstName,
		PushName:   pushName,
		IsBusiness: contact.BusinessName != "",
		AvatarURL:  avatarURL,
	}, nil
}

// PairWithPhone links this client using an 8-character pairing code instead
// of scanning the QR code. phoneNumber is the account's number in
// international format (digits only; "+" and spaces are ignored). The user
// types the returned code on the phone under Linked devices > Link with phone
// number instead.
func (a *Api) PairWithPhone(phoneNumber string) (string, error) {
	if a.waClient == nil {
		return "", fmt.Errorf("WhatsApp client is not initialized")
	}
	if a.waClient.Store.ID != nil {
		return "", fmt.Errorf("already logged in")
	}
	phoneNumber = normalizePairingPhone(phoneNumber)
	if len(phoneNumber) < 7 {
		return "", fmt.Errorf("enter the full number with country code, e.g. +43 660 1234567")
	}

	// Start from a fresh login websocket so the code gets the full validity
	// window. A manual disconnect keeps the QR channel from Login() alive, so
	// it still reports "success" once the phone confirms the code.
	if a.waClient.IsConnected() {
		a.waClient.Disconnect()
	}
	// whatsmeow recommends waiting for the first QR event: only then is the
	// login session fully set up on the server side.
	qrReady := make(chan struct{}, 1)
	handlerID := a.waClient.AddEventHandler(func(evt any) {
		if _, ok := evt.(*events.QR); ok {
			select {
			case qrReady <- struct{}{}:
			default:
			}
		}
	})
	defer a.waClient.RemoveEventHandler(handlerID)
	wa.EnsureCurrentVersion(a.ctx, false)
	if err := a.waClient.Connect(); err != nil {
		return "", fmt.Errorf("failed to connect: %w", err)
	}
	select {
	case <-qrReady:
	case <-time.After(10 * time.Second):
	case <-a.ctx.Done():
		return "", a.ctx.Err()
	}

	// WhatsApp validates the display name: it has to look like "Browser (OS)",
	// anything else is rejected with HTTP 400.
	code, err := a.waClient.PairPhone(a.ctx, phoneNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		a.waClient.Disconnect()
		return "", fmt.Errorf("failed to generate pairing code: %w", err)
	}
	return code, nil
}

// normalizePairingPhone keeps only the digits of a phone number and turns the
// international "00" prefix into the bare country code WhatsApp expects.
func normalizePairingPhone(phone string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	return strings.TrimPrefix(digits, "00")
}
