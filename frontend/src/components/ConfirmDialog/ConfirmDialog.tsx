import { useEffect, useRef } from "react"
import { useStore } from "../../store/store"
import "./ConfirmDialog.css"

export function ConfirmDialog() {
    const confirm = useStore((s) => s.confirm)
    const clearConfirm = useStore((s) => s.clearConfirm)
    const okRef = useRef<HTMLButtonElement>(null)

    useEffect(() => {
        if (!confirm) return
        requestAnimationFrame(() => okRef.current?.focus())

        const action = confirm.run
        function onKey(e: KeyboardEvent) {
            if (e.key === "Escape") {
                e.preventDefault()
                clearConfirm()
            } else if (e.key === "Enter") {
                e.preventDefault()
                clearConfirm()
                Promise.resolve(action()).catch((err) => console.error("confirm action failed:", err))
            }
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [confirm, clearConfirm])

    if (!confirm) return null

    function onConfirm() {
        const action = confirm!.run
        clearConfirm()
        Promise.resolve(action()).catch((err) => console.error("confirm action failed:", err))
    }

    return (
        <div className="confirm__overlay" onMouseDown={clearConfirm}>
            <div className="confirm" onMouseDown={(e) => e.stopPropagation()}>
                <p className="confirm__message">{confirm.message}</p>
                <div className="confirm__actions">
                    <button className="confirm__btn" onClick={clearConfirm}>
                        Cancel
                    </button>
                    <button ref={okRef} className="confirm__btn confirm__btn--danger" onClick={onConfirm}>
                        Confirm
                    </button>
                </div>
            </div>
        </div>
    )
}
