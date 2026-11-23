import { memo, useEffect, useMemo, useState } from "react"
import { Virtuoso } from "react-virtuoso"
import { FetchContacts, ResolvePhoneChat } from "../../../wailsjs/go/api/Api"
import { api } from "../../../wailsjs/go/models"
import { GoBackIcon } from "../../assets/svgs/header_icons"
import { SearchIcon } from "../../assets/svgs/settings_icons"
import { useCachedAvatar } from "../../lib/useCachedAvatar"
import { AVATAR_ICON_COLOR, getAvatarColor } from "../../lib/utils"

export interface NewChatTarget {
  jid: string
  name: string
}

/** Name shown for a contact: address-book name, else push name, else number. */
export function contactDisplayName(c: api.Contact): string {
  return c.full_name || c.push_name || c.short || c.phno
}

/**
 * Digits of a query that looks like a phone number ("+43 660 123", "0043…"),
 * or "" when it doesn't. Letters anywhere mean it's a name search.
 */
export function phoneQueryDigits(query: string): string {
  const q = query.trim()
  if (!/^[+\d(][\d\s()./-]*$/.test(q)) return ""
  const digits = q.replace(/\D/g, "")
  return digits.length >= 7 ? digits : ""
}

const isDark = () => document.documentElement.classList.contains("dark")

const ContactAvatar = memo(({ jid }: { jid: string }) => {
  const avatar = useCachedAvatar(jid)
  return (
    <div
      className="mr-4 flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-full"
      style={avatar ? undefined : { backgroundColor: getAvatarColor(jid, isDark()) }}
    >
      {avatar ? (
        <img src={avatar} alt="" className="h-full w-full object-cover" />
      ) : (
        <svg
          viewBox="0 0 48 48"
          className="h-full w-full p-1.5"
          fill={AVATAR_ICON_COLOR}
          aria-hidden
        >
          <path d="M24 23q-1.857 0-3.178-1.322Q19.5 20.357 19.5 18.5t1.322-3.178T24 14t3.178 1.322Q28.5 16.643 28.5 18.5t-1.322 3.178T24 23m-6.75 10q-.928 0-1.59-.66-.66-.662-.66-1.59v-.9q0-.956.492-1.758A3.3 3.3 0 0 1 16.8 26.87a16.7 16.7 0 0 1 3.544-1.308q1.8-.435 3.656-.436 1.856 0 3.656.436T31.2 26.87q.816.422 1.308 1.223T33 29.85v.9q0 .928-.66 1.59-.662.66-1.59.66z" />
        </svg>
      )}
    </div>
  )
})
ContactAvatar.displayName = "ContactAvatar"

interface NewChatPanelProps {
  onClose: () => void
  onOpenChat: (target: NewChatTarget) => void
  /** Prefills the search, e.g. with what was typed into the chat list search. */
  initialQuery?: string
}

/**
 * "New chat" sidebar panel: pick a contact or type a phone number to start a
 * conversation with someone you have no chat with yet.
 */
export function NewChatPanel({ onClose, onOpenChat, initialQuery = "" }: NewChatPanelProps) {
  const [contacts, setContacts] = useState<api.Contact[]>([])
  const [loading, setLoading] = useState(true)
  const [query, setQuery] = useState(initialQuery)
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    let cancelled = false
    FetchContacts()
      .then(list => {
        if (cancelled) return
        // Address-book contacts first (like the phone), then people only known
        // by their push name; alphabetical within each group.
        const sorted = (list || []).sort((a, b) => {
          const saved = Number(!a.full_name) - Number(!b.full_name)
          return saved || contactDisplayName(a).localeCompare(contactDisplayName(b))
        })
        setContacts(sorted)
      })
      .catch(err => !cancelled && setError(`Could not load contacts: ${String(err)}`))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

  // Esc closes the panel (capture phase so the chat's own Esc handler doesn't run).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation()
        onClose()
      }
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [onClose])

  const phoneDigits = phoneQueryDigits(query)

  const filtered = useMemo(() => {
    const term = query.trim().toLowerCase()
    if (!term) return contacts
    const digits = term.replace(/\D/g, "")
    return contacts.filter(c => {
      if (contactDisplayName(c).toLowerCase().includes(term)) return true
      if ((c.push_name || "").toLowerCase().includes(term)) return true
      return digits.length >= 3 && c.phno.replace(/\D/g, "").includes(digits)
    })
  }, [contacts, query])

  // Exact number match with a known contact: open it directly, no lookup needed.
  const exactMatch = phoneDigits
    ? contacts.find(c => c.phno.replace(/\D/g, "") === phoneDigits.replace(/^00/, ""))
    : undefined

  const openContact = (c: api.Contact) => onOpenChat({ jid: c.jid, name: contactDisplayName(c) })

  const startWithNumber = async () => {
    if (checking) return
    if (exactMatch) {
      openContact(exactMatch)
      return
    }
    setChecking(true)
    setError("")
    try {
      const c = await ResolvePhoneChat(query)
      openContact(c)
    } catch (err) {
      const msg = String(err).replace(/^Error:\s*/, "")
      setError(msg.charAt(0).toUpperCase() + msg.slice(1))
    } finally {
      setChecking(false)
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-white dark:bg-dark-bg">
      <div className="flex h-16 items-center gap-6 border-b border-gray-200 px-4 dark:border-white/5">
        <button
          onClick={onClose}
          className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          aria-label="Back to chats"
        >
          <GoBackIcon />
        </button>
        <h1 className="text-lg font-medium text-light-text dark:text-white">New chat</h1>
      </div>

      <div className="bg-light-bg px-3 py-2 dark:bg-dark-bg">
        <div className="flex items-center rounded-full bg-light-tertiary px-4 py-2 dark:bg-[#242626]">
          <div className="mr-4 text-gray-500 dark:text-gray-400">
            <SearchIcon />
          </div>
          <input
            type="text"
            autoFocus
            placeholder="Search name or number"
            className="w-full border-none bg-transparent text-sm text-light-text outline-none placeholder-gray-500 dark:text-dark-text"
            value={query}
            onChange={e => {
              setQuery(e.target.value)
              setError("")
            }}
            onKeyDown={e => {
              if (e.key !== "Enter") return
              if (phoneDigits) void startWithNumber()
              else if (filtered.length === 1) openContact(filtered[0])
            }}
          />
        </div>
      </div>

      {phoneDigits && (
        <button
          onClick={() => void startWithNumber()}
          disabled={checking}
          className="flex w-full items-center px-4 py-3 text-left hover:bg-gray-100 disabled:opacity-60 dark:hover:bg-[#1a1a1a]"
        >
          <div className="mr-4 flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-[#21c063] text-white">
            {checking ? (
              <div className="h-5 w-5 animate-spin rounded-full border-b-2 border-white" />
            ) : (
              <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor" aria-hidden>
                <path d="M20 15.5c-1.25 0-2.45-.2-3.57-.57a1 1 0 0 0-1.02.24l-2.2 2.2a15.05 15.05 0 0 1-6.59-6.59l2.2-2.21a1 1 0 0 0 .25-1A11.36 11.36 0 0 1 8.5 4a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1 17 17 0 0 0 17 17 1 1 0 0 0 1-1v-3.5a1 1 0 0 0-1-1z" />
              </svg>
            )}
          </div>
          <div className="min-w-0">
            <div className="truncate text-light-text dark:text-dark-text">
              {exactMatch ? contactDisplayName(exactMatch) : `Chat with ${query.trim()}`}
            </div>
            <div className="text-xs text-gray-500 dark:text-[#8696a0]">
              {checking ? "Checking the number…" : "Start a new chat with this number"}
            </div>
          </div>
        </button>
      )}

      {error && <div className="px-4 py-2 text-sm text-red-500">{error}</div>}

      <div className="px-4 pt-3 pb-1 text-sm text-[#008069] dark:text-[#21c063]">
        Contacts on WhatsApp
      </div>

      <div className="min-h-0 flex-1">
        {loading ? (
          <div className="flex justify-center p-6">
            <div className="h-6 w-6 animate-spin rounded-full border-b-2 border-green-500" />
          </div>
        ) : filtered.length === 0 ? (
          <div className="px-4 py-6 text-center text-sm text-gray-500 dark:text-[#8696a0]">
            {contacts.length === 0
              ? "No contacts synced yet. Type a phone number above to start a chat."
              : phoneDigits
                ? "No contact with this number. Use the entry above to start a chat."
                : "No contacts found"}
          </div>
        ) : (
          <Virtuoso
            style={{ height: "100%" }}
            data={filtered}
            computeItemKey={(_, c) => c.jid}
            itemContent={(_, c) => (
              <button
                onClick={() => openContact(c)}
                className="flex w-full items-center px-4 py-2.5 text-left hover:bg-gray-100 dark:hover:bg-[#1a1a1a]"
              >
                <ContactAvatar jid={c.jid} />
                <div className="min-w-0 flex-1 border-b border-gray-100 pb-2.5 dark:border-white/5">
                  <div className="truncate text-light-text dark:text-dark-text">
                    {contactDisplayName(c)}
                  </div>
                  <div className="truncate text-xs text-gray-500 dark:text-[#8696a0]">
                    {c.full_name && c.push_name && c.push_name !== c.full_name
                      ? `~${c.push_name} · ${c.phno}`
                      : c.phno}
                  </div>
                </div>
              </button>
            )}
          />
        )}
      </div>
    </div>
  )
}
