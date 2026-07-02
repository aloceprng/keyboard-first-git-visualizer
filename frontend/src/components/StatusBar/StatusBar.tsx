import { useStore } from "../../store/store"
import { postAction } from "../../api/client"
import type { ActionName } from "../../api/types"
import "./StatusBar.css"

const IN_PROGRESS_LABEL: Record<string, string> = {
    merge: "Merging",
    rebase: "Rebasing",
    "cherry-pick": "Cherry-picking",
    revert: "Reverting",
    bisect: "Bisecting",
}

interface StatusBarProps {
    reload: () => Promise<void>
}

export function StatusBar({ reload }: StatusBarProps) {
    const refs = useStore((s) => s.refs)
    const rows = useStore((s) => s.rows)

    if (!refs) return null

    const branch = refs.headBranch || "detached HEAD"
    const head = refs.head ? refs.head.slice(0, 7) : "—"
    const op = refs.inProgress

    async function act(action: ActionName) {
        try {
            await postAction({ action, args: {} })
            await reload()
        } catch (e) {
            console.error("action failed:", e)
        }
    }

    return (
        <footer className="status">
            <div className="status__left">
                <span className="status__branch">⎇ {branch}</span>
                <span className="status__sha">{head}</span>
                <span className="status__count">{rows.length} commits</span>
            </div>

            {op && (
                <div className="status__inprogress">
                    <span className="status__op">{IN_PROGRESS_LABEL[op] ?? op}…</span>
                    {op === "rebase" && (
                        <>
                            <button className="status__btn" onClick={() => act("rebase_continue")}>
                                Continue
                            </button>
                            <button className="status__btn status__btn--danger" onClick={() => act("rebase_abort")}>
                                Abort
                            </button>
                        </>
                    )}
                    {op === "cherry-pick" && (
                        <button className="status__btn status__btn--danger" onClick={() => act("cherry_pick_abort")}>
                            Abort
                        </button>
                    )}
                </div>
            )}
        </footer>
    )
}
