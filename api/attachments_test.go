package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mtypes "github.com/lugvitc/whats4linux/internal/types"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestExtensionForMime(t *testing.T) {
	cases := map[string]string{
		"image/jpeg":                 ".jpg",
		"video/mp4":                  ".mp4",
		"audio/ogg; codecs=opus":     ".ogg",
		"application/pdf":            ".pdf",
		"APPLICATION/PDF":            ".pdf",
		"application/x-unknown-type": ".bin",
		"":                           ".bin",
	}
	for in, want := range cases {
		if got := extensionForMime(in); got != want {
			t.Errorf("extensionForMime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":            "report.pdf",
		"../../etc/passwd":      "passwd",
		`C:\Users\x\notes.txt`:  "notes.txt",
		"  spaced name.docx  ":  "spaced name.docx",
		"a:b*c?.txt":            "a_b_c_.txt",
		"..":                    "",
		"":                      "",
		"bad\x00name\x1f.zip":   "bad_name_.zip",
		"trailing dots...":      "trailing dots",
		"dir/":                  "",
		"semi;colon & co.ods":   "semi;colon & co.ods",
		"ümlaut äöü résumé.pdf": "ümlaut äöü résumé.pdf",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 300) + ".pdf"
	if got := sanitizeFileName(long); len(got) > 200 || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("long name not shortened with extension kept: %d %q", len(got), got[len(got)-8:])
	}
}

func TestAttachmentFileName(t *testing.T) {
	ts := time.Date(2026, 11, 19, 14, 5, 9, 0, time.Local)
	if got := attachmentFileName("Invoice.pdf", "application/pdf", mtypes.MediaTypeDocument, ts); got != "Invoice.pdf" {
		t.Errorf("document keeps its name, got %q", got)
	}
	if got := attachmentFileName("README", "text/plain", mtypes.MediaTypeDocument, ts); got != "README.txt" {
		t.Errorf("missing extension is added, got %q", got)
	}
	if got := attachmentFileName("", "video/mp4", mtypes.MediaTypeVideo, ts); got != "WhatsApp Video 2026-11-19 at 14.05.09.mp4" {
		t.Errorf("generated video name, got %q", got)
	}
	if got := attachmentFileName("", "audio/ogg; codecs=opus", mtypes.MediaTypeAudio, ts); got != "WhatsApp Audio 2026-11-19 at 14.05.09.ogg" {
		t.Errorf("generated audio name, got %q", got)
	}
	if got := attachmentFileName("../x", "", mtypes.MediaTypeDocument, ts); got != "x.bin" {
		t.Errorf("path traversal stripped, got %q", got)
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	first := uniquePath(dir, "a.pdf")
	if first != filepath.Join(dir, "a.pdf") {
		t.Fatalf("first = %q", first)
	}
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := uniquePath(dir, "a.pdf")
	if second != filepath.Join(dir, "a (1).pdf") {
		t.Fatalf("second = %q", second)
	}
	if err := os.WriteFile(second, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if third := uniquePath(dir, "a.pdf"); third != filepath.Join(dir, "a (2).pdf") {
		t.Fatalf("third = %q", third)
	}
}

func TestDownloadDirFromUserDirs(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DOWNLOAD_DIR", "")

	if got := downloadDir(); got != filepath.Join(home, "Downloads") {
		t.Errorf("fallback = %q", got)
	}
	content := "# comment\nXDG_DESKTOP_DIR=\"$HOME/Schreibtisch\"\nXDG_DOWNLOAD_DIR=\"$HOME/Herunterladen\"\n"
	if err := os.WriteFile(filepath.Join(cfg, "user-dirs.dirs"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := downloadDir(); got != filepath.Join(home, "Herunterladen") {
		t.Errorf("user-dirs = %q", got)
	}
	// "$HOME/" alone means the directory is disabled; fall back.
	if err := os.WriteFile(filepath.Join(cfg, "user-dirs.dirs"), []byte("XDG_DOWNLOAD_DIR=\"$HOME/\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := downloadDir(); got != filepath.Join(home, "Downloads") {
		t.Errorf("disabled entry = %q", got)
	}
}

func TestResolveUploadMime(t *testing.T) {
	pngData := encodeTestPNG(t, 40, 30)
	cases := []struct {
		declared, name string
		data           []byte
		fallback, want string
	}{
		{"image/png", "x.png", nil, "image/jpeg", "image/png"},
		{"", "voice.opus", nil, "audio/ogg", "audio/ogg"},
		{"application/octet-stream", "talk.pdf", nil, "application/octet-stream", "application/pdf"},
		{"", "noext", pngData, "application/octet-stream", "image/png"},
		{"", "", nil, "video/mp4", "video/mp4"},
	}
	for _, c := range cases {
		if got := resolveUploadMime(c.declared, c.name, c.data, c.fallback); got != c.want {
			t.Errorf("resolveUploadMime(%q, %q) = %q, want %q", c.declared, c.name, got, c.want)
		}
	}
}

func TestImageDimensionsAndThumbnail(t *testing.T) {
	data := encodeTestPNG(t, 640, 480)
	w, h := imageDimensions(data)
	if w != 640 || h != 480 {
		t.Fatalf("dimensions = %dx%d", w, h)
	}
	thumb := generateThumbnail(data, "image/png")
	if len(thumb) == 0 {
		t.Fatal("no thumbnail generated")
	}
	if tw, th := imageDimensions(thumb); tw != 320 || th != 240 {
		t.Fatalf("thumbnail = %dx%d", tw, th)
	}
}

func TestSaveMediaRejectsMessagesWithoutMedia(t *testing.T) {
	a := newMediaTestAPI(t)
	if _, err := a.SaveMediaToFile("123@s.whatsapp.net", "text-only"); err == nil {
		t.Fatal("expected an error for a text message")
	}
	if _, err := a.SaveMediaToFile("123@s.whatsapp.net", "does-not-exist"); err == nil {
		t.Fatal("expected an error for a missing message")
	}
}

func TestNormalizePairingPhone(t *testing.T) {
	cases := map[string]string{
		"+43 660 123 4567": "436601234567",
		"0043 660 1234567": "436601234567",
		"(49) 151-234 567": "49151234567",
		"":                 "",
	}
	for in, want := range cases {
		if got := normalizePairingPhone(in); got != want {
			t.Errorf("normalizePairingPhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func encodeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProbeMediaFileWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	clip := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=10:duration=3",
		"-pix_fmt", "yuv420p", clip)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot create test clip: %v %s", err, out)
	}
	data, err := os.ReadFile(clip)
	if err != nil {
		t.Fatal(err)
	}
	p := probeMediaFile(context.Background(), data, true)
	if p.Width != 640 || p.Height != 360 {
		t.Errorf("size = %dx%d", p.Width, p.Height)
	}
	if p.Seconds != 3 {
		t.Errorf("seconds = %d", p.Seconds)
	}
	if w, h := imageDimensions(p.Thumbnail); w != 320 || h != 180 {
		t.Errorf("thumbnail = %dx%d (%d bytes)", w, h, len(p.Thumbnail))
	}
}

func TestSaveMediaToFileWritesCachedImage(t *testing.T) {
	a := newMediaTestAPI(t)
	dl := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", dl)

	chat, _ := types.ParseJID("123@s.whatsapp.net")
	sender, _ := types.ParseJID("456@s.whatsapp.net")
	ts := time.Date(2026, 11, 19, 9, 30, 0, 0, time.Local)
	info := types.MessageInfo{
		ID:            "img-1",
		Timestamp:     ts,
		MessageSource: types.MessageSource{Chat: chat, Sender: sender},
	}
	msg := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Mimetype:      proto.String("image/png"),
		DirectPath:    proto.String("/v/t62.7118-24/fake"),
		MediaKey:      []byte("0123456789abcdef0123456789abcdef"),
		FileSHA256:    []byte("sha"),
		FileEncSHA256: []byte("encsha"),
	}}
	if err := a.messageStore.InsertMessage(&info, msg, ""); err != nil {
		t.Fatal(err)
	}
	data := encodeTestPNG(t, 8, 8)
	if _, err := a.imageCache.SaveImage("img-1", data, "image/png", 8, 8); err != nil {
		t.Fatal(err)
	}

	path, err := a.SaveMediaToFile(chat.String(), "img-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dl, "WhatsApp Image 2026-11-19 at 09.30.00.png"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("saved bytes differ (err %v)", err)
	}
	if a.savedAttachment("img-1") != path {
		t.Fatal("saved path not remembered for OpenMedia")
	}

	// A second save must not overwrite the first file.
	path2, err := a.SaveMediaToFile(chat.String(), "img-1")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path2) != "WhatsApp Image 2026-11-19 at 09.30.00 (1).png" {
		t.Fatalf("second path = %q", path2)
	}
	if entries, _ := os.ReadDir(dl); len(entries) != 2 {
		t.Fatalf("expected exactly 2 files (no leftover .part), got %d", len(entries))
	}
}

func TestExistingDownloadIsReusedOnlyWhenHashMatches(t *testing.T) {
	a := newMediaTestAPI(t)
	dl := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", dl)

	content := []byte("%PDF-1.4 test document")
	sum := sha256.Sum256(content)
	chat, _ := types.ParseJID("123@s.whatsapp.net")
	info := types.MessageInfo{
		ID:            "doc-1",
		Timestamp:     time.Now(),
		MessageSource: types.MessageSource{Chat: chat, Sender: chat},
	}
	msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		Mimetype:      proto.String("application/pdf"),
		FileName:      proto.String("Plan.pdf"),
		DirectPath:    proto.String("/v/t62.7119-24/fake"),
		MediaKey:      []byte("0123456789abcdef0123456789abcdef"),
		FileSHA256:    sum[:],
		FileEncSHA256: []byte("encsha"),
	}}
	if err := a.messageStore.InsertMessage(&info, msg, ""); err != nil {
		t.Fatal(err)
	}

	if got := a.existingDownload(chat.String(), "doc-1"); got != "" {
		t.Fatalf("no file yet, got %q", got)
	}
	target := filepath.Join(dl, "Plan.pdf")
	if err := os.WriteFile(target, []byte("a different file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := a.existingDownload(chat.String(), "doc-1"); got != "" {
		t.Fatalf("different content must not be reused, got %q", got)
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := a.existingDownload(chat.String(), "doc-1"); got != target {
		t.Fatalf("matching file not reused, got %q", got)
	}
}

func insertTestMessage(t *testing.T, a *Api, id string, msg *waE2E.Message) {
	t.Helper()
	chat, _ := types.ParseJID("123@s.whatsapp.net")
	info := types.MessageInfo{
		ID:            id,
		Timestamp:     time.Date(2026, 11, 19, 11, 0, 0, 0, time.Local),
		MessageSource: types.MessageSource{Chat: chat, Sender: chat},
	}
	if err := a.messageStore.InsertMessage(&info, msg, ""); err != nil {
		t.Fatal(err)
	}
}

func TestSaveContactCardWritesVCard(t *testing.T) {
	a := newMediaTestAPI(t)
	dl := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", dl)

	vcard := "BEGIN:VCARD\nVERSION:3.0\nN:Muster;Anna;;;\nFN:Anna Muster\nTEL;type=CELL;waid=436601234567:+43 660 1234567\nEMAIL:anna@example.org\nEND:VCARD"
	insertTestMessage(t, a, "contact-1", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String("Anna Muster"),
		Vcard:       proto.String(vcard),
	}})

	path, err := a.SaveContactCard("123@s.whatsapp.net", "contact-1")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Anna Muster.vcf" {
		t.Fatalf("name = %q", filepath.Base(path))
	}
	got, _ := os.ReadFile(path)
	if string(got) != vcard+"\r\n" {
		t.Fatalf("vcf content = %q", got)
	}
	// Saving twice keeps both files.
	path2, err := a.SaveContactCard("123@s.whatsapp.net", "contact-1")
	if err != nil || filepath.Base(path2) != "Anna Muster (1).vcf" {
		t.Fatalf("second save = %q, %v", path2, err)
	}
}

func TestSaveContactCardArray(t *testing.T) {
	a := newMediaTestAPI(t)
	t.Setenv("XDG_DOWNLOAD_DIR", t.TempDir())

	insertTestMessage(t, a, "contacts-3", &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{
		DisplayName: proto.String("3 contacts"),
		Contacts: []*waE2E.ContactMessage{
			{DisplayName: proto.String("Anna"), Vcard: proto.String("BEGIN:VCARD\nVERSION:3.0\nFN:Anna\nEND:VCARD")},
			{DisplayName: proto.String("Ben"), Vcard: proto.String("BEGIN:VCARD\nVERSION:3.0\nFN:Ben\nEND:VCARD")},
			{DisplayName: proto.String("Cleo"), Vcard: proto.String("BEGIN:VCARD\nVERSION:3.0\nFN:Cleo\nEND:VCARD")},
		},
	}})
	path, err := a.SaveContactCard("123@s.whatsapp.net", "contacts-3")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Anna and 2 more.vcf" {
		t.Fatalf("name = %q", filepath.Base(path))
	}
	got, _ := os.ReadFile(path)
	if n := strings.Count(string(got), "BEGIN:VCARD"); n != 3 {
		t.Fatalf("expected 3 vCards, got %d: %q", n, got)
	}
}

func TestSaveContactCardFallsBackForOldMessages(t *testing.T) {
	a := newMediaTestAPI(t)
	t.Setenv("XDG_DOWNLOAD_DIR", t.TempDir())

	insertTestMessage(t, a, "old-contact", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String("Bob & Co; GmbH"),
		Vcard:       proto.String("BEGIN:VCARD\nVERSION:3.0\nFN:Bob\nTEL:+49 151 2345678\nEND:VCARD"),
	}})
	// Simulate a row written before vCards were stored.
	if err := a.messageStore.DeleteContactCardsForTest("old-contact"); err != nil {
		t.Fatal(err)
	}
	path, err := a.SaveContactCard("123@s.whatsapp.net", "old-contact")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "BEGIN:VCARD\r\nVERSION:3.0\r\nN:Bob & Co\\; GmbH;;;;\r\nFN:Bob & Co\\; GmbH\r\nTEL;TYPE=CELL:+49 151 2345678\r\nEND:VCARD\r\n"
	if string(got) != want {
		t.Fatalf("rebuilt vCard =\n%q\nwant\n%q", got, want)
	}
}

func TestSaveContactCardRejectsOtherMessages(t *testing.T) {
	a := newMediaTestAPI(t)
	if _, err := a.SaveContactCard("123@s.whatsapp.net", "text-only"); err == nil {
		t.Fatal("expected an error for a text message")
	}
}

func TestInternationalDigits(t *testing.T) {
	ok := map[string]string{
		"+43 660 1234567":   "436601234567",
		"0043 660 1234567":  "436601234567",
		"+49 (151) 2345678": "491512345678",
		"436601234567":      "436601234567",
	}
	for in, want := range ok {
		got, err := internationalDigits(in)
		if err != nil || got != want {
			t.Errorf("internationalDigits(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"0660 1234567", "6601234", "+43 12", "+1234567890123456", ""} {
		if got, err := internationalDigits(in); err == nil {
			t.Errorf("internationalDigits(%q) = %q, want an error", in, got)
		}
	}
}

func TestResolvePhoneChatNeedsLogin(t *testing.T) {
	if _, err := (&Api{}).ResolvePhoneChat("+43 660 1234567"); err == nil {
		t.Fatal("expected an error without a client")
	}
}
