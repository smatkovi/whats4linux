package store

import (
	"log"
	"sort"
	"strings"

	"github.com/lugvitc/whats4linux/internal/query"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// PinnedMessageText is what a "pinned a message" row shows in the chat.
const PinnedMessageText = `<i>📌 Pinned a message</i>`

var futureProofName = (&waE2E.FutureProofMessage{}).ProtoReflect().Descriptor().FullName()

// unwrapFutureProof returns the inner message of the first FutureProofMessage
// wrapper set on m (associatedChildMessage, groupMentionedMessage,
// pollCreationMessageV4, statusMentionMessage, …), or nil if there is none.
// WhatsApp keeps adding such wrappers, so they are found by type instead of
// being listed one by one. The outer messageContextInfo (which carries e.g.
// the album / HD association) is kept when the inner message has none.
func unwrapFutureProof(m *waE2E.Message) *waE2E.Message {
	var inner *waE2E.Message
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.Message().FullName() != futureProofName {
			return true
		}
		if fp, ok := v.Message().Interface().(*waE2E.FutureProofMessage); ok && fp.GetMessage() != nil {
			inner = fp.GetMessage()
			return false
		}
		return true
	})
	if inner != nil && inner.MessageContextInfo == nil && m.MessageContextInfo != nil {
		inner = proto.Clone(inner).(*waE2E.Message)
		inner.MessageContextInfo = m.MessageContextInfo
	}
	return inner
}

// skippedAssociations are child messages that only carry another copy of
// their parent (HD or HEVC upload, motion photo) or decorate something else.
// WhatsApp never shows them as messages of their own.
var skippedAssociations = map[waE2E.MessageAssociation_AssociationType]bool{
	waE2E.MessageAssociation_HD_IMAGE_DUAL_UPLOAD:   true,
	waE2E.MessageAssociation_HD_VIDEO_DUAL_UPLOAD:   true,
	waE2E.MessageAssociation_HEVC_VIDEO_DUAL_UPLOAD: true,
	waE2E.MessageAssociation_MOTION_PHOTO:           true,
	waE2E.MessageAssociation_STICKER_ANNOTATION:     true,
	waE2E.MessageAssociation_EVENT_COVER_IMAGE:      true,
	waE2E.MessageAssociation_POLL_ADD_OPTION:        true,
	waE2E.MessageAssociation_VIEW_ALL_REPLIES:       true,
	waE2E.MessageAssociation_BOT_PLUGIN:             true,
}

// envelopeFields are set on many messages next to the real content; a
// message with nothing else carries no visible content.
var envelopeFields = map[protoreflect.Name]bool{
	"messageContextInfo":                         true,
	"senderKeyDistributionMessage":               true,
	"fastRatchetKeySenderKeyDistributionMessage": true,
}

// contentFieldNames lists the populated top-level fields of m, envelope
// fields excluded, sorted for stable output.
func contentFieldNames(m *waE2E.Message) []string {
	var names []string
	if m == nil {
		return nil
	}
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if !envelopeFields[fd.Name()] {
			names = append(names, fd.JSONName())
		}
		return true
	})
	sort.Strings(names)
	return names
}

// isInvisibleMessage reports message kinds that WhatsApp doesn't show as a
// chat bubble: HD/motion copies of media, album headers (the photos arrive as
// their own messages), event RSVPs, encrypted reactions, message-secret
// bookkeeping and messages that carry only key distribution data.
func isInvisibleMessage(msg *waE2E.Message) bool {
	if assoc := msg.GetMessageContextInfo().GetMessageAssociation(); assoc != nil {
		t := assoc.GetAssociationType()
		if skippedAssociations[t] || strings.HasPrefix(t.String(), "STATUS_") {
			return true
		}
	}
	switch {
	case msg.GetAlbumMessage() != nil,
		msg.GetEncEventResponseMessage() != nil,
		msg.GetEncReactionMessage() != nil,
		msg.GetSecretEncryptedMessage() != nil,
		msg.GetStickerSyncRmrMessage() != nil,
		msg.GetMessageHistoryNotice() != nil:
		return true
	}
	return len(contentFieldNames(msg)) == 0
}

// unsupportedMessageCard renders a message type the app can't show yet as a
// small card naming it, and logs it so missing types can be added. Rows are
// never stored empty, which the UI used to show as "Unsupported Message Type".
func unsupportedMessageCard(messageID string, msg *waE2E.Message) string {
	names := contentFieldNames(msg)
	kind := strings.Join(names, ", ")
	if kind == "" {
		kind = "unknown"
	}
	log.Printf("[messages] unsupported message type %s in %s", kind, messageID)
	return `<div class="msg-card"><span class="msg-card-note">⚠️ This message type isn't supported yet (` +
		esc(kind) + `). Open WhatsApp on your phone to see it.</span></div>`
}

// hasVisibleContent reports whether a message row with text or media exists.
func (ms *MessageStore) hasVisibleContent(messageID string) bool {
	var one int
	return ms.db.QueryRow(query.SelectHasVisibleContent, messageID).Scan(&one) == nil
}
