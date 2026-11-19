package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lugvitc/whats4linux/internal/store"
	mtypes "github.com/lugvitc/whats4linux/internal/types"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"go.mau.fi/whatsmeow"
)

// preferredExtensions maps the MIME types WhatsApp commonly uses to the file
// extension people expect. mime.ExtensionsByType is only a fallback because
// its answer depends on /etc/mime.types and is often surprising (".jfif").
var preferredExtensions = map[string]string{
	"image/jpeg":         ".jpg",
	"image/jpg":          ".jpg",
	"image/png":          ".png",
	"image/gif":          ".gif",
	"image/webp":         ".webp",
	"image/heic":         ".heic",
	"video/mp4":          ".mp4",
	"video/3gpp":         ".3gp",
	"video/quicktime":    ".mov",
	"video/webm":         ".webm",
	"audio/ogg":          ".ogg",
	"audio/mpeg":         ".mp3",
	"audio/mp4":          ".m4a",
	"audio/aac":          ".aac",
	"audio/amr":          ".amr",
	"audio/wav":          ".wav",
	"audio/x-wav":        ".wav",
	"audio/webm":         ".weba",
	"application/pdf":    ".pdf",
	"application/zip":    ".zip",
	"application/msword": ".doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   ".docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"application/vnd.ms-excel":                        ".xls",
	"application/vnd.ms-powerpoint":                   ".ppt",
	"application/vnd.oasis.opendocument.text":         ".odt",
	"application/vnd.oasis.opendocument.spreadsheet":  ".ods",
	"application/vnd.oasis.opendocument.presentation": ".odp",
	"application/vnd.android.package-archive":         ".apk",
	"text/plain":       ".txt",
	"text/csv":         ".csv",
	"text/vcard":       ".vcf",
	"application/json": ".json",
}

// extensionForMime returns a file extension (with dot) for a MIME type, or
// ".bin" when nothing sensible is known.
func extensionForMime(mimeType string) string {
	base := strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(base, ';'); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	if ext, ok := preferredExtensions[base]; ok {
		return ext
	}
	if exts, err := mime.ExtensionsByType(base); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// defaultMimeFor returns the MIME type WhatsApp assumes when a message omits it.
func defaultMimeFor(mediaType mtypes.MediaType) string {
	switch mediaType {
	case mtypes.MediaTypeImage:
		return "image/jpeg"
	case mtypes.MediaTypeSticker:
		return "image/webp"
	case mtypes.MediaTypeVideo:
		return "video/mp4"
	case mtypes.MediaTypeAudio:
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

// sanitizeFileName strips path components and characters that are invalid or
// awkward in file names. Returns "" when nothing usable is left.
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Treat both separators as separators no matter which OS sent the file.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return '_'
		}
		switch r {
		case ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	// Keep the name within common file system limits (255 bytes).
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = strings.TrimSpace(name[:200-len(ext)]) + ext
	}
	return name
}

// attachmentFileName picks the name a downloaded attachment is saved under:
// the sender's file name for documents, otherwise a WhatsApp-style name built
// from the media kind, the timestamp and an extension matching the MIME type.
func attachmentFileName(storedName, mimeType string, mediaType mtypes.MediaType, timestamp time.Time) string {
	if name := sanitizeFileName(storedName); name != "" {
		if filepath.Ext(name) == "" {
			name += extensionForMime(mimeType)
		}
		return name
	}
	prefix := "WhatsApp File"
	switch mediaType {
	case mtypes.MediaTypeImage:
		prefix = "WhatsApp Image"
	case mtypes.MediaTypeSticker:
		prefix = "WhatsApp Sticker"
	case mtypes.MediaTypeVideo:
		prefix = "WhatsApp Video"
	case mtypes.MediaTypeAudio:
		prefix = "WhatsApp Audio"
	case mtypes.MediaTypeDocument:
		prefix = "WhatsApp Document"
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return prefix + " " + timestamp.Local().Format("2006-01-02 at 15.04.05") + extensionForMime(mimeType)
}

// downloadDir returns the user's XDG download directory (honouring
// ~/.config/user-dirs.dirs, so localised folders work), or ~/Downloads.
func downloadDir() string {
	home, _ := os.UserHomeDir()
	if dir := os.Getenv("XDG_DOWNLOAD_DIR"); dir != "" {
		return dir
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	if dir := readUserDirsDownload(filepath.Join(configHome, "user-dirs.dirs"), home); dir != "" {
		return dir
	}
	return filepath.Join(home, "Downloads")
}

func readUserDirsDownload(path, home string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		value, ok := strings.CutPrefix(line, "XDG_DOWNLOAD_DIR=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		value = strings.ReplaceAll(value, "$HOME", home)
		if value == "" || !filepath.IsAbs(value) {
			return ""
		}
		value = filepath.Clean(value)
		// "$HOME/" means the user disabled the directory.
		if value == filepath.Clean(home) {
			return ""
		}
		return value
	}
	return ""
}

// uniquePath returns dir/name, or "name (1).ext", "name (2).ext", ... when a
// file with that name already exists.
func uniquePath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
		return candidate
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 10000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, time.Now().UnixNano(), ext))
}

// friendlyDownloadError turns whatsmeow download errors into something a
// person can act on.
func friendlyDownloadError(err error) error {
	switch {
	case errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404),
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410):
		return fmt.Errorf("the file is no longer available on WhatsApp's servers (ask the sender to send it again)")
	case errors.Is(err, whatsmeow.ErrNoURLPresent):
		return fmt.Errorf("this message has no downloadable file")
	}
	return fmt.Errorf("download failed: %w", err)
}

// lookupMediaMessage finds a stored message with media by chat + ID, falling
// back to an ID-only lookup.
func (a *Api) lookupMediaMessage(chatJID, messageID string) (*store.ExtendedMessage, error) {
	if a.messageStore == nil {
		return nil, fmt.Errorf("message store is not ready")
	}
	var msg *store.ExtendedMessage
	var err error
	if chatJID != "" {
		msg, err = a.messageStore.GetMessageWithMedia(chatJID, messageID)
	}
	if msg == nil || err != nil {
		msg, err = a.messageStore.GetMessageWithMediaByID(messageID)
	}
	if err != nil || msg == nil {
		return nil, fmt.Errorf("message not found")
	}
	if msg.Media == nil {
		return nil, fmt.Errorf("this message has no attachment")
	}
	return msg, nil
}

// writeAttachment downloads the attachment of msg into target. Images and
// stickers come from the local image cache when possible; everything else is
// streamed to a temporary file next to the target and renamed into place, so
// large videos and documents never have to fit in memory or cross the
// JS bridge.
func (a *Api) writeAttachment(msg *store.ExtendedMessage, messageID, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(target), err)
	}

	if isImageOrSticker(msg) {
		if data, _, err := a.fetchImageBytes(messageID); err == nil && len(data) > 0 {
			return writeFileAtomic(target, data)
		}
	}

	if a.waClient == nil {
		return fmt.Errorf("WhatsApp client is not ready")
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.part")
	if err != nil {
		return fmt.Errorf("cannot create file: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err := a.waClient.DownloadToFile(a.ctx, msg.Media, tmp); err != nil {
		return friendlyDownloadError(err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	ok = true
	return nil
}

func writeFileAtomic(target string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.part")
	if err != nil {
		return fmt.Errorf("cannot create file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, 0o644)
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func mediaMimeOf(msg *store.ExtendedMessage) string {
	if m := strings.TrimSpace(msg.Media.GetMimetype()); m != "" {
		return m
	}
	return defaultMimeFor(msg.Media.GetMediaGeneralType())
}

func (a *Api) emitDownloadComplete(path string) {
	if a.ctx == nil || a.ctx.Value("events") == nil {
		return // not running inside Wails (tests)
	}
	runtime.EventsEmit(a.ctx, "download:complete", filepath.Base(path))
}

// SaveMediaToFile downloads the attachment of any media message (image, video,
// audio, document or sticker) into the user's download folder and returns
// the full path. Existing files are never overwritten; a " (n)" suffix is
// added instead.
func (a *Api) SaveMediaToFile(chatJID, messageID string) (string, error) {
	msg, err := a.lookupMediaMessage(chatJID, messageID)
	if err != nil {
		return "", err
	}
	name := attachmentFileName(msg.Media.GetFileName(), mediaMimeOf(msg), msg.Media.GetMediaGeneralType(), msg.Info.Timestamp)
	target := uniquePath(downloadDir(), name)
	if err := a.writeAttachment(msg, messageID, target); err != nil {
		return "", err
	}
	a.rememberSavedAttachment(messageID, target)
	a.emitDownloadComplete(target)
	return target, nil
}

// SaveMediaAs asks where to save the attachment and writes it there. Returns
// "" without error when the dialog is cancelled.
func (a *Api) SaveMediaAs(chatJID, messageID string) (string, error) {
	msg, err := a.lookupMediaMessage(chatJID, messageID)
	if err != nil {
		return "", err
	}
	mimeType := mediaMimeOf(msg)
	name := attachmentFileName(msg.Media.GetFileName(), mimeType, msg.Media.GetMediaGeneralType(), msg.Info.Timestamp)
	dir := downloadDir()
	_ = os.MkdirAll(dir, 0o755)
	target, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultDirectory:     dir,
		DefaultFilename:      name,
		Title:                "Save attachment",
		CanCreateDirectories: true,
	})
	if err != nil || target == "" {
		return "", err
	}
	if err := a.writeAttachment(msg, messageID, target); err != nil {
		return "", err
	}
	a.rememberSavedAttachment(messageID, target)
	a.emitDownloadComplete(target)
	return target, nil
}

// OpenMedia opens the attachment with the desktop's default application,
// downloading it to the download folder first if this session hasn't saved
// it yet.
func (a *Api) OpenMedia(chatJID, messageID string) (string, error) {
	path := a.savedAttachment(messageID)
	if path == "" {
		path = a.existingDownload(chatJID, messageID)
	}
	if path == "" {
		var err error
		if path, err = a.SaveMediaToFile(chatJID, messageID); err != nil {
			return "", err
		}
	}
	if err := openWithDesktop(path); err != nil {
		return path, err
	}
	return path, nil
}

// ShowInFolder opens the folder containing a previously saved attachment.
func (a *Api) ShowInFolder(path string) error {
	if path == "" {
		return fmt.Errorf("no file")
	}
	return openWithDesktop(filepath.Dir(path))
}

// openWithDesktop hands a file to the desktop's default application. Most
// openers return right after launching the app; if one fails within a few
// seconds (typically: no application registered for the type) that is
// reported, otherwise the launch counts as successful.
func openWithDesktop(path string) error {
	var cmd *exec.Cmd
	if opener, err := exec.LookPath("xdg-open"); err == nil {
		cmd = exec.Command(opener, path)
	} else if gio, err := exec.LookPath("gio"); err == nil {
		cmd = exec.Command(gio, "open", path)
	} else {
		return fmt.Errorf("no xdg-open found to open %s", path)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = "no application is set up to open this file type"
			}
			return fmt.Errorf("could not open %s: %s (the file is saved there)", filepath.Base(path), msg)
		}
		return nil
	case <-time.After(3 * time.Second):
		return nil
	}
}

// existingDownload returns the file in the download folder that already holds
// this attachment (same name and same SHA-256 as the message says), so
// opening a document again after a restart doesn't save another copy.
func (a *Api) existingDownload(chatJID, messageID string) string {
	msg, err := a.lookupMediaMessage(chatJID, messageID)
	if err != nil {
		return ""
	}
	want := msg.Media.GetFileSHA256()
	if len(want) != sha256.Size {
		return ""
	}
	name := attachmentFileName(msg.Media.GetFileName(), mediaMimeOf(msg), msg.Media.GetMediaGeneralType(), msg.Info.Timestamp)
	path := filepath.Join(downloadDir(), name)
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil || !bytes.Equal(h.Sum(nil), want) {
		return ""
	}
	a.rememberSavedAttachment(messageID, path)
	return path
}

func (a *Api) rememberSavedAttachment(messageID, path string) {
	a.savedAttachments.Store(messageID, path)
}

func (a *Api) savedAttachment(messageID string) string {
	v, ok := a.savedAttachments.Load(messageID)
	if !ok {
		return ""
	}
	path := v.(string)
	if _, err := os.Stat(path); err != nil {
		a.savedAttachments.Delete(messageID)
		return ""
	}
	return path
}

// ----------------------------------------------------------------------------
// Sending
// ----------------------------------------------------------------------------

// uploadMimeByExt is the reverse of preferredExtensions for the formats
// browsers most often leave without a type.
var uploadMimeByExt = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp", ".heic": "image/heic",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".3gp": "video/3gpp",
	".mov": "video/quicktime", ".webm": "video/webm", ".mkv": "video/x-matroska",
	".ogg": "audio/ogg", ".opus": "audio/ogg", ".oga": "audio/ogg",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac",
	".amr": "audio/amr", ".wav": "audio/wav", ".flac": "audio/flac",
	".pdf": "application/pdf", ".zip": "application/zip",
	".doc": "application/msword", ".xls": "application/vnd.ms-excel",
	".ppt":  "application/vnd.ms-powerpoint",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
	".apk":  "application/vnd.android.package-archive",
	".txt":  "text/plain", ".csv": "text/csv", ".vcf": "text/vcard", ".json": "application/json",
}

// resolveUploadMime returns a usable MIME type for an outgoing file. Browsers
// leave File.type empty for many formats, so fall back to the extension and
// then to content sniffing.
func resolveUploadMime(declared, fileName string, data []byte, fallback string) string {
	declared = strings.TrimSpace(declared)
	if declared != "" && declared != "application/octet-stream" {
		return declared
	}
	if ext := strings.ToLower(filepath.Ext(fileName)); ext != "" {
		if m, ok := uploadMimeByExt[ext]; ok {
			return m
		}
		if m := mime.TypeByExtension(ext); m != "" {
			if i := strings.IndexByte(m, ';'); i >= 0 {
				m = m[:i]
			}
			return m
		}
	}
	if len(data) > 0 {
		if m := http.DetectContentType(data); m != "" && !strings.HasPrefix(m, "application/octet-stream") {
			if i := strings.IndexByte(m, ';'); i >= 0 {
				m = strings.TrimSpace(m[:i])
			}
			return m
		}
	}
	if declared != "" {
		return declared
	}
	return fallback
}

// imageDimensions returns the pixel size of an encoded image, or zeros.
func imageDimensions(data []byte) (uint32, uint32) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0
	}
	return uint32(cfg.Width), uint32(cfg.Height)
}

type mediaProbe struct {
	Width     uint32
	Height    uint32
	Seconds   uint32
	Thumbnail []byte
}

// probeMediaFile uses ffprobe/ffmpeg (when installed) to read the size and
// duration of a video or audio file and grab a JPEG preview frame for videos.
// Everything is optional: without ffmpeg the message is sent without them.
func probeMediaFile(ctx context.Context, data []byte, wantThumbnail bool) mediaProbe {
	var res mediaProbe
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return res
	}
	tmp, err := os.CreateTemp("", "w4l-probe-*")
	if err != nil {
		return res
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return res
	}
	_ = tmp.Close()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-show_entries", "stream=codec_type,width,height:format=duration",
		"-of", "json", tmp.Name(),
	).Output()
	if err == nil {
		var parsed struct {
			Streams []struct {
				CodecType string `json:"codec_type"`
				Width     int    `json:"width"`
				Height    int    `json:"height"`
			} `json:"streams"`
			Format struct {
				Duration string `json:"duration"`
			} `json:"format"`
		}
		if json.Unmarshal(out, &parsed) == nil {
			for _, s := range parsed.Streams {
				if s.CodecType == "video" && s.Width > 0 && s.Height > 0 {
					res.Width, res.Height = uint32(s.Width), uint32(s.Height)
					break
				}
			}
			if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil && d > 0 {
				res.Seconds = uint32(d + 0.5)
			}
		}
	}

	if wantThumbnail {
		if ffmpeg, err := exec.LookPath("ffmpeg"); err == nil {
			thumb, err := exec.CommandContext(ctx, ffmpeg,
				"-v", "error", "-i", tmp.Name(),
				"-frames:v", "1",
				"-vf", "scale='min(320,iw)':-2",
				"-f", "image2", "-c:v", "mjpeg", "-q:v", "6", "pipe:1",
			).Output()
			if err == nil && len(thumb) > 0 {
				res.Thumbnail = thumb
			}
		}
	}
	return res
}

// logProbe keeps a trace of what we could learn about outgoing media.
func logProbe(kind string, p mediaProbe) {
	log.Printf("[attachments] %s: %dx%d, %ds, thumbnail %d bytes", kind, p.Width, p.Height, p.Seconds, len(p.Thumbnail))
}

// ----------------------------------------------------------------------------
// Shared contacts (vCards)
// ----------------------------------------------------------------------------

// contactFileName names the .vcf file after the contact, or after the first
// contact plus a count when several were shared at once.
func contactFileName(cards []store.ContactCard, ts time.Time) string {
	first := ""
	if len(cards) > 0 {
		first = sanitizeFileName(cards[0].DisplayName)
	}
	switch {
	case first == "" && len(cards) > 1:
		if ts.IsZero() {
			ts = time.Now()
		}
		return "WhatsApp Contacts " + ts.Local().Format("2006-01-02 at 15.04.05") + ".vcf"
	case first == "":
		return "Contact.vcf"
	case len(cards) == 2:
		return first + " and 1 more.vcf"
	case len(cards) > 2:
		return fmt.Sprintf("%s and %d more.vcf", first, len(cards)-1)
	}
	return first + ".vcf"
}

// joinVCards concatenates the vCards into one .vcf file body; every address
// book accepts several BEGIN:VCARD blocks in one file.
func joinVCards(cards []store.ContactCard) []byte {
	var b strings.Builder
	for _, c := range cards {
		v := strings.TrimSpace(c.VCard)
		if v == "" {
			v = strings.TrimSpace(store.MinimalVCard(c.DisplayName, ""))
		}
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

func (a *Api) contactCards(messageID string) ([]store.ContactCard, time.Time, error) {
	if a.messageStore == nil {
		return nil, time.Time{}, fmt.Errorf("message store is not ready")
	}
	cards, ts, reconstructed, err := a.messageStore.GetContactCards(messageID)
	if err != nil {
		return nil, ts, err
	}
	if reconstructed {
		log.Printf("[contacts] %s predates stored vCards; saving name and number only", messageID)
	}
	return cards, ts, nil
}

// SaveContactCard saves the contact(s) shared in a message as a .vcf file in
// the download folder and returns its path.
func (a *Api) SaveContactCard(chatJID, messageID string) (string, error) {
	cards, ts, err := a.contactCards(messageID)
	if err != nil {
		return "", err
	}
	dir := downloadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := uniquePath(dir, contactFileName(cards, ts))
	if err := writeFileAtomic(target, joinVCards(cards)); err != nil {
		return "", err
	}
	a.rememberSavedAttachment(messageID, target)
	a.emitDownloadComplete(target)
	return target, nil
}

// SaveContactCardAs asks where to save the contact(s) as a .vcf file.
func (a *Api) SaveContactCardAs(chatJID, messageID string) (string, error) {
	cards, ts, err := a.contactCards(messageID)
	if err != nil {
		return "", err
	}
	dir := downloadDir()
	_ = os.MkdirAll(dir, 0o755)
	target, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultDirectory:     dir,
		DefaultFilename:      contactFileName(cards, ts),
		Title:                "Save contact",
		CanCreateDirectories: true,
		Filters:              []runtime.FileFilter{{DisplayName: "vCard (*.vcf)", Pattern: "*.vcf"}},
	})
	if err != nil || target == "" {
		return "", err
	}
	if err := writeFileAtomic(target, joinVCards(cards)); err != nil {
		return "", err
	}
	a.rememberSavedAttachment(messageID, target)
	a.emitDownloadComplete(target)
	return target, nil
}

// OpenContactCard saves the contact(s) (once per session) and opens the .vcf
// with the default application, usually the address book's import.
func (a *Api) OpenContactCard(chatJID, messageID string) (string, error) {
	path := a.savedAttachment(messageID)
	if path == "" {
		var err error
		if path, err = a.SaveContactCard(chatJID, messageID); err != nil {
			return "", err
		}
	}
	return path, openWithDesktop(path)
}
