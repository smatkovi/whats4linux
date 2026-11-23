package query

const (
	// CreateContactCardsTable keeps the raw vCards of shared contacts so they
	// can be saved as .vcf files; the message text only holds a summary card.
	CreateContactCardsTable = `
	CREATE TABLE IF NOT EXISTS contact_cards (
		message_id TEXT NOT NULL,
		idx INTEGER NOT NULL,
		display_name TEXT,
		vcard TEXT,
		PRIMARY KEY (message_id, idx)
	);
	`

	DeleteContactCardsByMessageID = `
	DELETE FROM contact_cards WHERE message_id = ?;
	`

	InsertContactCard = `
	INSERT OR REPLACE INTO contact_cards (message_id, idx, display_name, vcard)
	VALUES (?, ?, ?, ?);
	`

	SelectContactCardsByMessageID = `
	SELECT display_name, vcard FROM contact_cards WHERE message_id = ? ORDER BY idx;
	`

	FillPinnedMessageText = `
	UPDATE messages SET text = ? WHERE type = 1 AND (text IS NULL OR text = '');
	`

	SelectHasVisibleContent = `
	SELECT 1 FROM messages WHERE message_id = ? AND ((text IS NOT NULL AND text <> '') OR has_media = 1);
	`

	SelectMessageTextByID = `
	SELECT text, timestamp FROM messages WHERE message_id = ?;
	`
)
