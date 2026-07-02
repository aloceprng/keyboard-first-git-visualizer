import { useEffect, useState } from "react"
import { useStore } from "../../store/store"
import { fetchCommit } from "../../api/client"
import type { CommitDetail } from "../../api/types"
import "./Panel.css"

function stripRef(ref: string): string {
    return ref
        .replace("refs/heads/", "")
        .replace("refs/remotes/", "")
        .replace("refs/tags/", "")
}

export function Panel() {
    const selectedSHA = useStore((s) => s.selectedSHA)
    const [detail, setDetail] = useState<CommitDetail | null>(null)
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        if (!selectedSHA) {
            setDetail(null)
            return
        }
        let cancelled = false
        setError(null)
        setDetail(null)
        fetchCommit(selectedSHA)
            .then((d) => {
                if (!cancelled) setDetail(d)
            })
            .catch((e) => {
                if (!cancelled) setError(e?.message ?? String(e))
            })
        return () => {
            cancelled = true
        }
    }, [selectedSHA])

    if (!selectedSHA) return null

    const namedRefs = detail?.refs.filter((r) => r !== "HEAD") ?? []

    return (
        <aside className="panel">
            {error && <div className="panel__error">{error}</div>}
            {!detail && !error && <div className="panel__muted">loading…</div>}
            {detail && (
                <>
                    <div className="panel__sha">{detail.short}</div>
                    <div className="panel__subject">{detail.subject}</div>
                    {detail.body && <pre className="panel__body">{detail.body}</pre>}
                    <div className="panel__muted">
                        {detail.author.name} · {new Date(detail.timestamp).toLocaleString()}
                    </div>
                    {detail.stats && (
                        <div className="panel__stats">
                            <span className="panel__add">+{detail.stats.insertions}</span>{" "}
                            <span className="panel__del">−{detail.stats.deletions}</span>{" "}
                            <span className="panel__muted">{detail.stats.files} files</span>
                        </div>
                    )}
                    {namedRefs.length > 0 && (
                        <div className="panel__refs">
                            {namedRefs.map((r) => (
                                <span key={r} className="panel__pill">
                                    {stripRef(r)}
                                </span>
                            ))}
                        </div>
                    )}
                </>
            )}
        </aside>
    )
}
