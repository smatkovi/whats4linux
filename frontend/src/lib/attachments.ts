import { OpenMedia, SaveMediaAs, SaveMediaToFile } from "../../wailsjs/go/api/Api"
import { useUIStore } from "../store/useUIStore"

/** Shows a short message in the toast stack at the bottom right. */
export function toast(message: string, ms = 4000) {
  const { addNotification, removeNotification } = useUIStore.getState()
  const id = addNotification(message)
  setTimeout(() => removeNotification(id), ms)
}

function errorText(err: unknown): string {
  const text = String(err ?? "")
  return text.replace(/^Error:\s*/, "") || "unknown error"
}

/** Kinds of message content that carry a downloadable attachment. */
export function hasAttachment(content: any): boolean {
  return !!(
    content?.imageMessage ||
    content?.videoMessage ||
    content?.audioMessage ||
    content?.documentMessage ||
    content?.stickerMessage
  )
}

/**
 * Saves the attachment of a message into the download folder. The
 * "Downloaded: …" toast comes from the backend's download:complete event.
 */
export async function saveAttachment(chatId: string, messageId: string): Promise<string | null> {
  try {
    return await SaveMediaToFile(chatId, messageId)
  } catch (err) {
    toast(`Download failed: ${errorText(err)}`, 6000)
    return null
  }
}

/** Asks for a location, then saves the attachment there. */
export async function saveAttachmentAs(chatId: string, messageId: string): Promise<string | null> {
  try {
    return (await SaveMediaAs(chatId, messageId)) || null
  } catch (err) {
    toast(`Saving failed: ${errorText(err)}`, 6000)
    return null
  }
}

/** Downloads the attachment if needed and opens it with the default app. */
export async function openAttachment(chatId: string, messageId: string): Promise<string | null> {
  try {
    return await OpenMedia(chatId, messageId)
  } catch (err) {
    toast(`Cannot open file: ${errorText(err)}`, 6000)
    return null
  }
}

// Formats WhatsApp shows inline. Anything else is sent as a document so the
// recipient still gets the file unchanged.
const INLINE_IMAGE = new Set(["image/jpeg", "image/png", "image/webp"])
const INLINE_VIDEO = new Set(["video/mp4", "video/3gpp"])
const INLINE_AUDIO = new Set([
  "audio/ogg",
  "audio/mpeg",
  "audio/mp4",
  "audio/aac",
  "audio/amr",
  "audio/x-m4a",
])

/**
 * Picks the WhatsApp message type for an outgoing file: "image", "video",
 * "audio" or "document". forceDocument mirrors WhatsApp's "Document" picker,
 * which sends photos and videos uncompressed as files.
 */
export function classifyAttachment(file: File, forceDocument = false): string {
  if (forceDocument) return "document"
  const type = (file.type || "").toLowerCase()
  if (INLINE_IMAGE.has(type)) return "image"
  if (INLINE_VIDEO.has(type)) return "video"
  if (INLINE_AUDIO.has(type)) return "audio"
  return "document"
}
