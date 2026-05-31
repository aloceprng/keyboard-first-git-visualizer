import { Command, type Child } from "@tauri-apps/plugin-shell"

let child: Child | null = null

async function isAlreadyRunning(): Promise<boolean> {
    try {
        const resp = await fetch("http://127.0.0.1:7832/ping", {
            signal: AbortSignal.timeout(300),
        })
        return resp.ok
    } catch {
        return false
    }
}

export async function startBackend(repoPath: string): Promise<void> {
    if (await isAlreadyRunning()) {
        console.log("reattaching to existing backend")
        return
    }

    return new Promise((resolve, reject) => {
        const command = Command.sidecar("binaries/gitvis", [repoPath])

        command.stdout.on("data", (line: string) => {
            console.log("[backend]", line)
            if (line.trim() === "ready") {
                resolve()
            }
        })

        command.stderr.on("data", (line: string) => {
            console.error("[backend error]", line)
        })

        command.on("error", (err) => {
            reject(new Error(`backend failed to start: ${err}`))
        })

        command.spawn()
            .then((c) => { child = c })
            .catch(reject)
    })
}

export async function stopBackend(): Promise<void> {
    if (child) {
        await child.kill()
        child = null
    }
}