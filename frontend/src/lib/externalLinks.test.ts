import { describe, expect, it } from "vitest"
import { externalUrl } from "./externalLinks"

describe("externalUrl", () => {
  it("opens web, mail and phone links outside the app", () => {
    expect(externalUrl("https://example.com/a?b=1")).toBe("https://example.com/a?b=1")
    expect(externalUrl("http://example.com")).toBe("http://example.com")
    expect(externalUrl("HTTPS://EXAMPLE.COM")).toBe("HTTPS://EXAMPLE.COM")
    expect(externalUrl("mailto:someone@example.com")).toBe("mailto:someone@example.com")
    expect(externalUrl("tel:+43660123")).toBe("tel:+43660123")
    expect(externalUrl("  https://example.com ")).toBe("https://example.com")
  })

  it("leaves in-app and unsafe links alone", () => {
    expect(externalUrl("#section")).toBeNull()
    expect(externalUrl("/settings")).toBeNull()
    expect(externalUrl("javascript:alert(1)")).toBeNull()
    expect(externalUrl("file:///etc/passwd")).toBeNull()
    expect(externalUrl("")).toBeNull()
    expect(externalUrl(null)).toBeNull()
  })
})
