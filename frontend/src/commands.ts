import type { RefsResponse, ActionRequest } from "./api/types"
import { postAction } from "./api/client"

export type PaletteMode = "commands" | "search" | "branch"

export type CommandContext = "commit" | "rebase" | "cherry-pick"

// Everything a command needs to act on app state. Built fresh by the palette
// on each invocation so commands never import the store directly.
export interface CommandCtx {
    selectedSHA: string | null
    refs: RefsResponse | null
    reload: () => Promise<void>
    selectCommit: (sha: string | null) => void
    setMode: (mode: PaletteMode) => void
    close: () => void
    confirm: (message: string, run: () => void | Promise<void>) => void
    openRepoPicker: () => void
}

export interface Command {
    id: string
    label: string
    shortcut?: string
    context?: CommandContext
    run: (ctx: CommandCtx) => void | Promise<void>
}

// run a git action, refresh the graph, and close the palette
async function perform(c: CommandCtx, req: ActionRequest): Promise<void> {
    await postAction(req)
    await c.reload()
    c.close()
}

export const COMMANDS: Command[] = [
    // ── global — always visible ────────────────────────────────────────────
    {
        id: "jump-head",
        label: "Jump to HEAD",
        shortcut: "G",
        run: (c) => {
            if (c.refs?.head) c.selectCommit(c.refs.head)
            c.close()
        },
    },
    { id: "search", label: "Search Commits…", run: (c) => c.setMode("search") },
    { id: "go-branch", label: "Go to Branch…", run: (c) => c.setMode("branch") },
    { id: "open-repo", label: "Open Repo…", run: (c) => { c.openRepoPicker(); c.close() } },
    {
        id: "refresh",
        label: "Refresh Graph",
        shortcut: "R",
        run: async (c) => {
            await c.reload()
            c.close()
        },
    },
    { id: "fetch", label: "Fetch (origin)", run: (c) => perform(c, { action: "fetch", args: {} }) },

    {
        id: "merge",
        label: "Merge Branch into Current…",
        run: (c) => {
            const branch = window.prompt("Branch to merge into the current branch:")?.trim()
            if (!branch) return
            return perform(c, { action: "merge", args: { branch } })
        },
    },
    {
        id: "rebase",
        label: "Rebase Current onto…",
        run: (c) => {
            const onto = window.prompt("Rebase the current branch onto:")?.trim()
            if (!onto) return
            return perform(c, { action: "rebase", args: { onto } })
        },
    },
    {
        id: "branch-rename",
        label: "Rename Current Branch…",
        run: (c) => {
            const from = c.refs?.headBranch
            if (!from) return // detached HEAD — nothing to rename
            const to = window.prompt(`Rename branch "${from}" to:`)?.trim()
            if (!to) return
            return perform(c, { action: "branch_rename", args: { from, to } })
        },
    },
    {
        id: "branch-delete",
        label: "Delete Branch…",
        run: (c) => {
            const name = window.prompt("Branch to delete:")?.trim()
            if (!name) return
            c.confirm(`Delete branch "${name}"?`, () =>
                perform(c, { action: "branch_delete", args: { name }, confirm: true }),
            )
            c.close()
        },
    },
    {
        id: "tag-delete",
        label: "Delete Tag…",
        run: (c) => {
            const name = window.prompt("Tag to delete:")?.trim()
            if (!name) return
            c.confirm(`Delete tag "${name}"?`, () =>
                perform(c, { action: "tag_delete", args: { name }, confirm: true }),
            )
            c.close()
        },
    },

    {
        id: "stash",
        label: "Stash Changes…",
        run: (c) => {
            const message = window.prompt("Stash message (optional):")
            if (message === null) return // cancelled
            const m = message.trim()
            return perform(c, { action: "stash", args: m ? { message: m } : {} })
        },
    },
    { id: "stash-pop", label: "Stash Pop", run: (c) => perform(c, { action: "stash_pop", args: {} }) },
    { id: "stash-drop", label: "Stash Drop", run: (c) => perform(c, { action: "stash_drop", args: {} }) },

    // ── rebase in progress ─────────────────────────────────────────────────
    {
        id: "rebase-continue",
        label: "Rebase: Continue",
        context: "rebase",
        run: (c) => perform(c, { action: "rebase_continue", args: {} }),
    },
    {
        id: "rebase-abort",
        label: "Rebase: Abort",
        context: "rebase",
        run: (c) => perform(c, { action: "rebase_abort", args: {} }),
    },

    // ── cherry-pick in progress ────────────────────────────────────────────
    {
        id: "cherry-pick-abort",
        label: "Cherry-pick: Abort",
        context: "cherry-pick",
        run: (c) => perform(c, { action: "cherry_pick_abort", args: {} }),
    },

    // ── commit context — visible only when a commit is selected ─────────────
    {
        id: "copy-sha",
        label: "Copy SHA",
        shortcut: "Y",
        context: "commit",
        run: (c) => {
            if (c.selectedSHA) void navigator.clipboard.writeText(c.selectedSHA)
            c.close()
        },
    },
    {
        id: "checkout",
        label: "Checkout This Commit",
        context: "commit",
        run: (c) => {
            if (!c.selectedSHA) return
            // detach: checking out a bare commit detaches HEAD
            return perform(c, { action: "checkout", args: { target: c.selectedSHA, detach: "true" } })
        },
    },
    {
        id: "branch-here",
        label: "Create Branch Here…",
        context: "commit",
        run: (c) => {
            if (!c.selectedSHA) return
            const name = window.prompt("New branch name:")?.trim()
            if (!name) return
            return perform(c, { action: "branch_create", args: { name, start: c.selectedSHA } })
        },
    },
    {
        id: "tag-here",
        label: "Tag This Commit…",
        context: "commit",
        run: (c) => {
            if (!c.selectedSHA) return
            const name = window.prompt("Tag name:")?.trim()
            if (!name) return
            return perform(c, { action: "tag", args: { name, sha: c.selectedSHA } })
        },
    },
    {
        id: "cherry-pick",
        label: "Cherry-pick This Commit",
        context: "commit",
        run: (c) => {
            if (!c.selectedSHA) return
            return perform(c, { action: "cherry_pick", args: { sha: c.selectedSHA } })
        },
    },
    {
        id: "revert",
        label: "Revert This Commit",
        context: "commit",
        run: (c) => {
            if (!c.selectedSHA) return
            return perform(c, { action: "revert", args: { sha: c.selectedSHA } })
        },
    },
    {
        id: "reset-soft",
        label: "Reset --soft to This Commit",
        context: "commit",
        run: (c) => {
            if (!c.selectedSHA) return
            return perform(c, { action: "reset_soft", args: { sha: c.selectedSHA } })
        },
    },
    {
        id: "reset-mixed",
        label: "Reset --mixed to This Commit",
        context: "commit",
        run: (c) => {
            if (!c.selectedSHA) return
            return perform(c, { action: "reset_mixed", args: { sha: c.selectedSHA } })
        },
    },
    {
        id: "reset-hard",
        label: "Reset --hard to This Commit",
        context: "commit",
        run: (c) => {
            const sha = c.selectedSHA
            if (!sha) return
            c.confirm(
                `Reset --hard to ${sha.slice(0, 7)}? This permanently discards uncommitted changes.`,
                () => perform(c, { action: "reset_hard", args: { sha }, confirm: true }),
            )
            c.close()
        },
    },
]

// Filters commands by their context against the current selection and the
// repo's in-progress operation.
export function visibleCommands(selectedSHA: string | null, refs: RefsResponse | null): Command[] {
    return COMMANDS.filter((cmd) => {
        switch (cmd.context) {
            case "commit":
                return selectedSHA !== null
            case "rebase":
                return refs?.inProgress === "rebase"
            case "cherry-pick":
                return refs?.inProgress === "cherry-pick"
            default:
                return true
        }
    })
}
