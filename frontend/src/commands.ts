import type { RefsResponse, Row, ActionRequest } from "./api/types"
import { postAction } from "./api/client"
import { laneColor } from "./components/Graph/EdgeRenderer"

export type PaletteTab = "global" | "commit" | "branch"

export interface PromptSpec {
    title: string
    placeholder?: string
    initial?: string
    submitLabel?: string
    allowEmpty?: boolean
    run: (value: string, ctx: CommandCtx) => void | Promise<void>
}

export interface CommandCtx {
    selectedSHA: string | null
    selectedRow: Row | null
    refs: RefsResponse | null
    rows: Row[]
    reload: () => Promise<void>
    selectCommit: (sha: string | null) => void
    close: () => void
    confirm: (message: string, run: () => void | Promise<void>) => void
    prompt: (spec: PromptSpec) => void
    enterSearch: () => void
    openRepoPicker: () => void
}

export interface Command {
    id: string
    label: string
    // single-key hint rendered as one keycap
    shortcut?: string
    // multi-key chord, e.g. ["ctrl", "O"]
    keys?: string[]
    // section heading the row sits under
    group?: string
    // leading dot colour — used for branch checkouts
    dot?: string
    // marks a destructive action (red affordance)
    danger?: boolean
    run: (ctx: CommandCtx) => void | Promise<void>
}

// run a git action, surface any conflict/error the backend streams back 
// (these arrive as events on a 200 response, not as HTTP errors), refresh, then close
async function perform(c: CommandCtx, req: ActionRequest): Promise<void> {
    const events = await postAction(req)
    const bad = events.find((e) => e.type === "error" || e.type === "conflict")
    if (bad) {
        await c.reload()
        throw new Error(
            bad.type === "conflict"
                ? `Conflicts in ${bad.files?.length ?? 0} file(s) — resolve, then continue`
                : bad.data || "action failed",
        )
    }
    await c.reload()
    c.close()
}

// local branches, current one first, each tagged with whether it's HEAD
function branches(refs: RefsResponse | null) {
    const current = refs?.headBranch || ""
    return (refs?.refs ?? [])
        .filter((r) => r.type === "branch")
        .map((r) => ({ name: r.name.replace("refs/heads/", ""), sha: r.sha }))
        .map((b) => ({ ...b, isCurrent: b.name === current }))
        .sort((a, b) => Number(b.isCurrent) - Number(a.isCurrent) || a.name.localeCompare(b.name))
}

function tags(refs: RefsResponse | null) {
    return (refs?.refs ?? [])
        .filter((r) => r.type === "tag")
        .map((r) => ({ name: r.name.replace("refs/tags/", ""), sha: r.sha }))
}

// colour a branch dot by the lane its tip sits on, so it matches the graph.
function tipColor(sha: string, rows: Row[]): string {
    const row = rows.find((r) => r.sha === sha)
    return row ? laneColor(row.lane) : "var(--cp-text-muted)"
}

// global
function globalCommands(c: CommandCtx): Command[] {
    const cmds: Command[] = [
        {
            id: "jump-head",
            label: "Jump to HEAD",
            shortcut: "G",
            group: "Navigate",
            run: (c) => {
                if (c.refs?.head) c.selectCommit(c.refs.head)
                c.close()
            },
        },
        { id: "search-commits", label: "Search Commits…", shortcut: "/", group: "Navigate", run: (c) => c.enterSearch() },
        { id: "refresh", label: "Refresh Graph", shortcut: "R", group: "Repository", run: async (c) => { await c.reload(); c.close() } },
        { id: "open-repo", label: "Open Repo…", keys: ["ctrl", "O"], group: "Repository", run: (c) => { c.openRepoPicker(); c.close() } },
        { id: "fetch", label: "Fetch from origin", group: "Repository", run: (c) => perform(c, { action: "fetch", args: {} }) },
        { id: "fetch-prune", label: "Fetch & Prune", group: "Repository", run: (c) => perform(c, { action: "fetch", args: { prune: "true" } }) },
    ]

    // checkout any branch
    for (const b of branches(c.refs)) {
        if (b.isCurrent) continue
        cmds.push({
            id: `checkout-${b.name}`,
            label: `Checkout ${b.name}`,
            group: "Checkout",
            dot: tipColor(b.sha, c.rows),
            run: (c) => perform(c, { action: "checkout", args: { target: b.name } }),
        })
    }

    cmds.push(
        {
            id: "stash",
            label: "Stash Changes…",
            group: "Working Tree",
            run: (c) =>
                c.prompt({
                    title: "Stash message",
                    placeholder: "optional label…",
                    allowEmpty: true,
                    run: (msg, c) => perform(c, { action: "stash", args: msg ? { message: msg } : {} }),
                }),
        },
        { id: "stash-pop", label: "Stash Pop", group: "Working Tree", run: (c) => perform(c, { action: "stash_pop", args: {} }) },
        {
            id: "stash-drop",
            label: "Stash Drop",
            group: "Working Tree",
            danger: true,
            run: (c) => { c.confirm("Drop the most recent stash entry? This cannot be undone.", () => perform(c, { action: "stash_drop", args: {} })); c.close() },
        },
    )

    // delete any tag
    for (const t of tags(c.refs)) {
        cmds.push({
            id: `tag-delete-${t.name}`,
            label: `Delete Tag ${t.name}…`,
            group: "Tags",
            danger: true,
            run: (c) => { c.confirm(`Delete tag "${t.name}"?`, () => perform(c, { action: "tag_delete", args: { name: t.name }, confirm: true })); c.close() },
        })
    }

    // resume / abort whatever the repo is mid-way through
    if (c.refs?.inProgress === "rebase") {
        cmds.push(
            { id: "rebase-continue", label: "Rebase: Continue", group: "In Progress", run: (c) => perform(c, { action: "rebase_continue", args: {} }) },
            { id: "rebase-abort", label: "Rebase: Abort", group: "In Progress", danger: true, run: (c) => perform(c, { action: "rebase_abort", args: {} }) },
        )
    }
    if (c.refs?.inProgress === "cherry-pick") {
        cmds.push({ id: "cherry-pick-abort", label: "Cherry-pick: Abort", group: "In Progress", danger: true, run: (c) => perform(c, { action: "cherry_pick_abort", args: {} }) })
    }

    return cmds
}

// commit
function commitCommands(c: CommandCtx): Command[] {
    const sha = c.selectedSHA
    if (!sha) return []
    const short = sha.slice(0, 7)
    return [
        {
            id: "copy-sha",
            label: "Copy SHA",
            shortcut: "Y",
            group: "This Commit",
            run: (c) => { void navigator.clipboard.writeText(sha); c.close() },
        },
        { id: "checkout", label: "Checkout (detached)", group: "This Commit", run: (c) => perform(c, { action: "checkout", args: { target: sha, detach: "true" } }) },
        {
            id: "branch-here",
            label: "Branch from Here…",
            group: "This Commit",
            run: (c) => c.prompt({ title: "New branch name", placeholder: "feature/…", run: (name, c) => perform(c, { action: "branch_create", args: { name, start: sha } }) }),
        },
        {
            id: "branch-checkout-here",
            label: "Branch & Checkout from Here…",
            group: "This Commit",
            run: (c) => c.prompt({ title: "New branch name", placeholder: "feature/…", run: (name, c) => perform(c, { action: "branch_create", args: { name, start: sha, checkout: "true" } }) }),
        },
        {
            id: "tag-here",
            label: "Tag This Commit…",
            group: "This Commit",
            run: (c) => c.prompt({ title: "Tag name", placeholder: "v1.0.0", run: (name, c) => perform(c, { action: "tag", args: { name, sha } }) }),
        },
        { id: "cherry-pick", label: "Cherry-pick onto Current", group: "This Commit", run: (c) => perform(c, { action: "cherry_pick", args: { sha } }) },
        { id: "revert", label: "Revert This Commit", group: "This Commit", run: (c) => perform(c, { action: "revert", args: { sha } }) },

        { id: "reset-soft", label: "Reset --soft to Here", group: "Reset", run: (c) => perform(c, { action: "reset_soft", args: { sha } }) },
        { id: "reset-mixed", label: "Reset --mixed to Here", group: "Reset", run: (c) => perform(c, { action: "reset_mixed", args: { sha } }) },
        {
            id: "reset-hard",
            label: "Reset --hard to Here",
            group: "Reset",
            danger: true,
            run: (c) => { c.confirm(`Reset --hard to ${short}? This permanently discards uncommitted changes.`, () => perform(c, { action: "reset_hard", args: { sha }, confirm: true })); c.close() },
        },
    ]
}

// branch
function branchCommands(c: CommandCtx): Command[] {
    const cmds: Command[] = []
    const list = branches(c.refs)
    const current = c.refs?.headBranch || ""
    const others = list.filter((b) => !b.isCurrent)

    for (const b of others) {
        cmds.push({
            id: `b-checkout-${b.name}`,
            label: `Checkout ${b.name}`,
            group: "Checkout",
            dot: tipColor(b.sha, c.rows),
            run: (c) => perform(c, { action: "checkout", args: { target: b.name } }),
        })
    }

    if (current) {
        for (const b of others) {
            cmds.push(
                { id: `b-rebase-${b.name}`, label: `Rebase ${current} onto ${b.name}`, group: "Integrate", run: (c) => perform(c, { action: "rebase", args: { onto: b.name } }) },
                { id: `b-merge-${b.name}`, label: `Merge ${b.name} into ${current}`, group: "Integrate", run: (c) => perform(c, { action: "merge", args: { branch: b.name } }) },
                { id: `b-merge-sq-${b.name}`, label: `Merge ${b.name} (squash)`, group: "Integrate", run: (c) => perform(c, { action: "merge", args: { branch: b.name, strategy: "squash" } }) },
            )
        }
        cmds.push({
            id: "b-rename",
            label: `Rename ${current}…`,
            group: "Manage",
            run: (c) => c.prompt({ title: `Rename "${current}" to`, initial: current, run: (to, c) => perform(c, { action: "branch_rename", args: { from: current, to } }) }),
        })
    }

    for (const b of others) {
        cmds.push({
            id: `b-delete-${b.name}`,
            label: `Delete ${b.name}…`,
            group: "Manage",
            danger: true,
            run: (c) => { c.confirm(`Delete branch "${b.name}"? Unmerged work will be lost.`, () => perform(c, { action: "branch_delete", args: { name: b.name, force: "true" }, confirm: true })); c.close() },
        })
    }

    return cmds
}

// build the command list for a tab against the current context.
export function buildCommands(tab: PaletteTab, c: CommandCtx): Command[] {
    switch (tab) {
        case "global":
            return globalCommands(c)
        case "commit":
            return commitCommands(c)
        case "branch":
            return branchCommands(c)
    }
}
