import { describe, expect, it } from "vitest"
import { contactDisplayName, phoneQueryDigits } from "./NewChatPanel"

describe("phoneQueryDigits", () => {
  it("recognises phone numbers in various notations", () => {
    expect(phoneQueryDigits("+43 660 1234567")).toBe("436601234567")
    expect(phoneQueryDigits("0043-660-1234567")).toBe("00436601234567")
    expect(phoneQueryDigits(" (0660) 123 4567 ")).toBe("06601234567")
  })

  it("ignores names and too-short numbers", () => {
    expect(phoneQueryDigits("Anna")).toBe("")
    expect(phoneQueryDigits("Anna 0660")).toBe("")
    expect(phoneQueryDigits("+43 66")).toBe("")
    expect(phoneQueryDigits("")).toBe("")
  })
})

describe("contactDisplayName", () => {
  it("prefers the address-book name, then push name, then number", () => {
    const base = {
      jid: "1@s.whatsapp.net",
      phno: "+43 660 1",
      short: "",
      is_business: false,
      avatar_url: "",
    }
    expect(contactDisplayName({ ...base, full_name: "Anna M", push_name: "anna" })).toBe("Anna M")
    expect(contactDisplayName({ ...base, full_name: "", push_name: "anna" })).toBe("anna")
    expect(contactDisplayName({ ...base, full_name: "", push_name: "" })).toBe("+43 660 1")
  })
})
