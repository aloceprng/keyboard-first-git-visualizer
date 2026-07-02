import React from "react"
import ReactDOM from "react-dom/client"
import App from "./App"
import { startBackend } from "./lib/backend"
import "./index.css"

async function init() {
    try {
        // hardcode for now — later use @tauri-apps/plugin-dialog
        // to let the user pick a folder
        const repoPath = "."
        await startBackend(repoPath)
    } catch (err) {
        console.error("Failed to start backend:", err)
        return
    }

    ReactDOM.createRoot(document.getElementById("root")!).render(
        <React.StrictMode>
            <App />
        </React.StrictMode>
    )
}

init()