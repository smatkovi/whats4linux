import { describe, expect, it } from "vitest"
import { classifyAttachment, hasAttachment, isContactCardHtml } from "./attachments"

const file = (name: string, type: string) => new File(["x"], name, { type })

describe("classifyAttachment", () => {
  it("sends formats WhatsApp shows inline as media", () => {
    expect(classifyAttachment(file("a.jpg", "image/jpeg"))).toBe("image")
    expect(classifyAttachment(file("a.mp4", "video/mp4"))).toBe("video")
    expect(classifyAttachment(file("a.mp3", "audio/mpeg"))).toBe("audio")
  })

  it("sends everything else, and anything picked as document, as document", () => {
    expect(classifyAttachment(file("a.pdf", "application/pdf"))).toBe("document")
    expect(classifyAttachment(file("a.svg", "image/svg+xml"))).toBe("document")
    expect(classifyAttachment(file("a.mkv", "video/x-matroska"))).toBe("document")
    expect(classifyAttachment(file("noext", ""))).toBe("document")
    expect(classifyAttachment(file("a.jpg", "image/jpeg"), true)).toBe("document")
  })
})

describe("message helpers", () => {
  it("recognises the backend's contact card HTML", () => {
    // Same markup as contactCard() in internal/store/special.go.
    expect(isContactCardHtml('<div class="msg-card">👤 <b>Anna</b><br>+43 660 1</div>')).toBe(true)
    expect(isContactCardHtml('<div class="msg-card">📍 <a class="msg-link">x</a></div>')).toBe(
      false,
    )
    expect(isContactCardHtml("👤 just text")).toBe(false)
  })

  it("detects attachment-bearing content", () => {
    expect(hasAttachment({ documentMessage: {} })).toBe(true)
    expect(hasAttachment({ conversation: "hi" })).toBe(false)
    expect(hasAttachment(undefined)).toBe(false)
  })
})
