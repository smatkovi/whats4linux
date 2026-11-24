import { BrowserOpenURL } from "../../wailsjs/runtime/runtime"

/** Link schemes handed to the desktop (browser, mail client, dialer). */
const EXTERNAL_SCHEME = /^(https?|mailto|tel):/i

/**
 * The URL to open outside the app for a link's href, or null for anything
 * else (in-app anchors, javascript:, relative paths).
 */
export function externalUrl(href: string | null | undefined): string | null {
  const url = (href ?? "").trim()
  return EXTERNAL_SCHEME.test(url) ? url : null
}

function linkUrlAt(target: EventTarget | null): string | null {
  const anchor = (target as Element | null)?.closest?.("a[href]")
  return anchor ? externalUrl(anchor.getAttribute("href")) : null
}

/**
 * Opens every external link in the system browser instead of the app's own
 * webview, which has no address bar or back button: a page loaded there
 * replaces the whole app. Covers left click (also with Ctrl/Shift), middle
 * click, which WebKitGTK otherwise follows in the same view, and
 * window.open(), which otherwise does nothing. Listeners run in the capture
 * phase so no component can stop them. Returns a function that removes them.
 *
 * Don't preventDefault() the middle-button mousedown as well: WebKitGTK 2.x
 * then crashes in its GTK event handling (reproduced under Xvfb).
 */
export function installExternalLinkHandler(
  open: (url: string) => void = BrowserOpenURL,
): () => void {
  const onClick = (e: MouseEvent) => {
    // Primary button arrives as "click", the middle button as "auxclick";
    // right click is left to the context menu.
    if ((e.type === "click" && e.button !== 0) || (e.type === "auxclick" && e.button !== 1)) return
    const url = linkUrlAt(e.target)
    if (!url) return
    e.preventDefault()
    open(url)
  }
  const originalOpen = window.open
  window.open = function (url?: string | URL, target?: string, features?: string) {
    const external = externalUrl(url?.toString())
    if (external) {
      open(external)
      return null
    }
    return originalOpen.call(window, url, target, features)
  }

  window.addEventListener("click", onClick, true)
  window.addEventListener("auxclick", onClick, true)
  return () => {
    window.removeEventListener("click", onClick, true)
    window.removeEventListener("auxclick", onClick, true)
    window.open = originalOpen
  }
}
