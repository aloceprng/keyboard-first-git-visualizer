import "./Kbd.css"

interface KbdProps {
    children: React.ReactNode
    size?: number
}

export function Kbd({ children, size = 22 }: KbdProps) {
    return (
        <kbd
            className="gv-kbd"
            style={{ height: size, minWidth: size, fontSize: Math.round(size * 0.5) }}
        >
            {children}
        </kbd>
    )
}
