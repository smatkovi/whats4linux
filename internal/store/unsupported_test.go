package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// noLIDs is a LID store that knows no mappings.
type noLIDs struct{ store.LIDStore }

func (noLIDs) GetLIDForPN(context.Context, types.JID) (types.JID, error) { return types.JID{}, nil }
func (noLIDs) GetPNForLID(context.Context, types.JID) (types.JID, error) { return types.JID{}, nil }

func processTestEvent(t *testing.T, ms *MessageStore, id string, msg *waE2E.Message) string {
	t.Helper()
	chat, _ := types.ParseJID("123-456@g.us")
	sender, _ := types.ParseJID("4366012345@s.whatsapp.net")
	evt := &events.Message{
		Info: types.MessageInfo{
			ID:            id,
			Timestamp:     time.Now(),
			MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsGroup: true},
		},
		Message: msg,
	}
	return ms.ProcessMessageEvent(context.Background(), noLIDs{}, evt, "")
}

func assoc(t waE2E.MessageAssociation_AssociationType) *waE2E.MessageContextInfo {
	return &waE2E.MessageContextInfo{MessageAssociation: &waE2E.MessageAssociation{
		AssociationType:  t.Enum(),
		ParentMessageKey: &waCommon.MessageKey{ID: proto.String("PARENT")},
	}}
}

func image() *waE2E.ImageMessage {
	return &waE2E.ImageMessage{
		Mimetype:      proto.String("image/jpeg"),
		Caption:       proto.String("from the album"),
		DirectPath:    proto.String("/v/t62.7118-24/x"),
		MediaKey:      []byte("k"),
		FileSHA256:    []byte("s"),
		FileEncSHA256: []byte("e"),
	}
}

// visibleText returns the stored text and whether the row would be listed.
func visibleText(t *testing.T, ms *MessageStore, id string) (string, bool) {
	t.Helper()
	var text string
	var hasMedia bool
	err := ms.db.QueryRow(`SELECT COALESCE(text, ''), has_media FROM messages WHERE message_id = ?`, id).Scan(&text, &hasMedia)
	if err != nil {
		return "", false
	}
	return text, text != "" || hasMedia
}

func TestInvisibleMessageKindsCreateNoRow(t *testing.T) {
	ms := newTestMessageStore(t)
	cases := map[string]*waE2E.Message{
		"hd-copy": {AssociatedChildMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			ImageMessage: image(), MessageContextInfo: assoc(waE2E.MessageAssociation_HD_IMAGE_DUAL_UPLOAD),
		}}},
		// Association on the outer message only must survive unwrapping.
		"hd-copy-outer-ctx": {
			AssociatedChildMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: image()}},
			MessageContextInfo:     assoc(waE2E.MessageAssociation_HD_IMAGE_DUAL_UPLOAD),
		},
		"motion-photo": {AssociatedChildMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{}, MessageContextInfo: assoc(waE2E.MessageAssociation_MOTION_PHOTO),
		}}},
		"album-header":  {AlbumMessage: &waE2E.AlbumMessage{ExpectedImageCount: proto.Uint32(4)}},
		"rsvp":          {EncEventResponseMessage: &waE2E.EncEventResponseMessage{}},
		"enc-reaction":  {EncReactionMessage: &waE2E.EncReactionMessage{}},
		"key-only":      {SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{GroupID: proto.String("g")}, MessageContextInfo: &waE2E.MessageContextInfo{}},
		"empty":         {},
		"secret-update": {SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{}},
	}
	for id, msg := range cases {
		if got := processTestEvent(t, ms, id, msg); got != "" {
			t.Errorf("%s: stored as %q, want skipped", id, got)
		}
		if _, err := ms.GetDecodedMessage("123-456@g.us", id); err == nil {
			t.Errorf("%s: a row exists", id)
		}
	}
}

func TestWrappedContentIsUnwrapped(t *testing.T) {
	ms := newTestMessageStore(t)

	// Album photos arrive wrapped in associatedChildMessage.
	album := &waE2E.Message{AssociatedChildMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		ImageMessage: image(), MessageContextInfo: assoc(waE2E.MessageAssociation_MEDIA_ALBUM),
	}}}
	if processTestEvent(t, ms, "album-1", album) == "" {
		t.Fatal("album photo was skipped")
	}
	dm, err := ms.GetDecodedMessage("123-456@g.us", "album-1")
	if err != nil || dm.Content.ImageMessage == nil || dm.Content.ImageMessage.Caption != "from the album" {
		t.Fatalf("album photo not stored as image: %+v, %v", dm, err)
	}

	// Text in a group-mention wrapper.
	mention := &waE2E.Message{GroupMentionedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		Conversation: proto.String("hello @group"),
	}}}
	processTestEvent(t, ms, "mention-1", mention)
	if text, ok := visibleText(t, ms, "mention-1"); !ok || !strings.Contains(text, "hello") {
		t.Fatalf("group mention text = %q", text)
	}

	// Newer poll versions render as polls.
	pollV5 := &waE2E.Message{PollCreationMessageV5: &waE2E.PollCreationMessage{
		Name:    proto.String("Pizza?"),
		Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("yes")}, {OptionName: proto.String("no")}},
	}}
	processTestEvent(t, ms, "poll-5", pollV5)
	if text, _ := visibleText(t, ms, "poll-5"); !strings.Contains(text, "Pizza?") {
		t.Fatalf("poll v5 text = %q", text)
	}
	if !IsPollCreation(pollV5) {
		t.Fatal("IsPollCreation(v5) = false")
	}
}

func TestUnknownTypesGetANamedCardInsteadOfAnEmptyRow(t *testing.T) {
	ms := newTestMessageStore(t)
	msg := &waE2E.Message{RequestPhoneNumberMessage: &waE2E.RequestPhoneNumberMessage{}}
	if processTestEvent(t, ms, "odd-1", msg) == "" {
		t.Fatal("unknown type was dropped")
	}
	text, ok := visibleText(t, ms, "odd-1")
	if !ok || !strings.Contains(text, "requestPhoneNumberMessage") {
		t.Fatalf("fallback card = %q", text)
	}
}

func TestFallbackDoesNotOverwriteStoredContent(t *testing.T) {
	ms := newTestMessageStore(t)
	processTestEvent(t, ms, "dup-1", &waE2E.Message{Conversation: proto.String("real text")})
	processTestEvent(t, ms, "dup-1", &waE2E.Message{RequestPhoneNumberMessage: &waE2E.RequestPhoneNumberMessage{}})
	if text, _ := visibleText(t, ms, "dup-1"); !strings.Contains(text, "real text") {
		t.Fatalf("content was overwritten: %q", text)
	}
}

func TestPinEventsAndOldEmptyRows(t *testing.T) {
	ms := newTestMessageStore(t)
	pin := &waE2E.Message{PinInChatMessage: &waE2E.PinInChatMessage{
		Key:  &waCommon.MessageKey{ID: proto.String("target")},
		Type: waE2E.PinInChatMessage_PIN_FOR_ALL.Enum(),
	}, MessageContextInfo: &waE2E.MessageContextInfo{MessageAddOnDurationInSecs: proto.Uint32(86400)}}
	processTestEvent(t, ms, "pin-1", pin)
	if text, _ := visibleText(t, ms, "pin-1"); text != PinnedMessageText {
		t.Fatalf("pin text = %q", text)
	}

	// Rows an older version stored without text or media are not listed.
	insertTestMessage(t, ms, "old-empty", "123-456@g.us", time.Now().Unix(), "")
	if _, err := ms.db.Exec(`UPDATE messages SET text = '' WHERE message_id = 'old-empty'`); err != nil {
		t.Fatal(err)
	}
	insertTestMessage(t, ms, "old-text", "123-456@g.us", time.Now().Unix(), "")
	msgs, err := ms.GetDecodedMessagesPaged("123-456@g.us", 0, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Info.ID == "old-empty" {
			t.Fatal("empty legacy row is listed")
		}
	}
	found := false
	for _, m := range msgs {
		found = found || m.Info.ID == "old-text"
	}
	if !found {
		t.Fatal("normal row missing from the list")
	}
}
