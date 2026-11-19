import { useState } from "react"
import { Login, PairWithPhone } from "../../wailsjs/go/api/Api"

type Mode = "qr" | "phone"

export function LoginScreen({
  canvasRef,
  status,
}: {
  canvasRef: React.RefObject<HTMLCanvasElement | null>
  status?: string
}) {
  const [mode, setMode] = useState<Mode>("qr")
  const [phone, setPhone] = useState("")
  const [pairCode, setPairCode] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")

  // whatsmeow closes the login socket once all QR codes have been shown.
  const qrExpired = status === "timeout" && mode === "qr"

  const requestCode = async () => {
    setLoading(true)
    setError("")
    try {
      setPairCode(await PairWithPhone(phone))
    } catch (err) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }

  const reloadQR = () => {
    void Login().catch(err => setError(String(err)))
  }

  const formattedCode =
    pairCode.length === 8 ? `${pairCode.slice(0, 4)}-${pairCode.slice(4)}` : pairCode

  return (
    <div className="min-h-screen flex items-center justify-center bg-light-secondary dark:bg-dark-bg p-4">
      <div className="w-full max-w-225 h-130 bg-white dark:bg-gray-900 rounded-lg shadow-xl flex overflow-hidden border border-gray-100 dark:border-gray-800">
        {/* LEFT */}
        <div className="flex-1 p-12 flex flex-col justify-center">
          <div className="flex items-center gap-3 mb-8">
            <img
              src="https://upload.wikimedia.org/wikipedia/commons/thumb/6/6b/WhatsApp.svg/1022px-WhatsApp.svg.png"
              className="w-10 h-10"
              alt="WhatsApp Logo"
            />
            <h1 className="text-2xl font-light text-gray-700 dark:text-gray-200">
              Log in to WhatsApp
            </h1>
          </div>

          <ol className="list-decimal pl-6 space-y-4 text-gray-600 dark:text-gray-400 text-lg leading-relaxed">
            <li>Open WhatsApp on your phone</li>
            <li>
              Tap <b className="font-semibold text-gray-800 dark:text-gray-200">Menu</b> or{" "}
              <b className="font-semibold text-gray-800 dark:text-gray-200">Settings</b>
            </li>
            <li>
              Select{" "}
              <b className="font-semibold text-gray-800 dark:text-gray-200">Linked Devices</b>
            </li>
            {mode === "qr" ? (
              <li>Point your phone at this screen</li>
            ) : (
              <li>
                Tap{" "}
                <b className="font-semibold text-gray-800 dark:text-gray-200">
                  Link with phone number instead
                </b>{" "}
                and enter the code shown here
              </li>
            )}
          </ol>

          <button
            type="button"
            onClick={() => {
              setMode(mode === "qr" ? "phone" : "qr")
              setError("")
            }}
            className="mt-8 self-start text-light-primary dark:text-green-400 hover:underline"
          >
            {mode === "qr" ? "Link with phone number instead" : "Link with QR code instead"}
          </button>
        </div>

        {/* RIGHT */}
        <div className="flex-1 p-12 flex flex-col items-center justify-center border-l border-gray-100 dark:border-gray-800">
          {/* Keep the canvas mounted so QR updates keep drawing while hidden. */}
          <div className={mode === "qr" ? "flex flex-col items-center" : "hidden"}>
            <div className="relative bg-white p-2 rounded-lg shadow-sm mb-6">
              <canvas ref={canvasRef} className={`size-66 ${qrExpired ? "opacity-10" : ""}`} />
              {qrExpired && (
                <button
                  type="button"
                  onClick={reloadQR}
                  className="absolute inset-0 m-auto size-32 rounded-full bg-green-600 hover:bg-green-700 text-white font-medium"
                >
                  Reload QR code
                </button>
              )}
            </div>

            <p className="text-gray-500 dark:text-gray-400 text-center mb-4">
              Scan this QR code with the WhatsApp app
            </p>

            <p className="text-sm font-medium text-light-primary dark:text-green-400 animate-pulse uppercase tracking-wide">
              {status}
            </p>
          </div>

          {mode === "phone" && (
            <div className="w-full max-w-80 flex flex-col gap-3">
              {!pairCode ? (
                <>
                  <label
                    htmlFor="pair-phone"
                    className="text-gray-600 dark:text-gray-400 text-sm font-medium"
                  >
                    Phone number with country code
                  </label>
                  <input
                    id="pair-phone"
                    type="tel"
                    autoFocus
                    placeholder="+43 660 1234567"
                    value={phone}
                    onChange={e => setPhone(e.target.value)}
                    onKeyDown={e => {
                      if (e.key === "Enter" && phone && !loading) void requestCode()
                    }}
                    className="p-3 rounded-lg bg-gray-50 dark:bg-gray-800 border border-gray-300 dark:border-gray-700 text-gray-900 dark:text-white outline-none focus:border-green-500"
                  />
                  <button
                    type="button"
                    onClick={() => void requestCode()}
                    disabled={loading || !phone}
                    className="bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white p-3 rounded-lg font-semibold transition"
                  >
                    {loading ? "Connecting…" : "Get pairing code"}
                  </button>
                </>
              ) : (
                <div className="flex flex-col items-center gap-3">
                  <p className="text-gray-600 dark:text-gray-300 text-center">
                    Enter this code on your phone:
                  </p>
                  <div className="text-3xl font-mono tracking-widest bg-gray-50 dark:bg-gray-800 px-6 py-3 rounded-lg border border-green-500 text-green-600 dark:text-green-400 select-all">
                    {formattedCode}
                  </div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 text-center">
                    WhatsApp → Linked devices → Link a device → Link with phone number instead
                  </p>
                  {status === "timeout" && (
                    <p className="text-sm text-red-500 text-center">
                      The code has expired. Get a new one.
                    </p>
                  )}
                  <button
                    type="button"
                    onClick={() => setPairCode("")}
                    className="text-sm text-light-primary dark:text-green-400 hover:underline"
                  >
                    Use a different number or get a new code
                  </button>
                </div>
              )}
              {error && <span className="text-red-500 text-sm break-words">{error}</span>}
            </div>
          )}

          {mode === "qr" && error && <span className="text-red-500 text-sm mt-2">{error}</span>}
        </div>
      </div>
    </div>
  )
}
