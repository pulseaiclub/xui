# XUI

A high-performance, differential terminal UI (TUI) toolkit for Go 

XUI focuses on minimal output and correctness in real-world terminals: CJK-width-aware rendering, mouse/paste/focus/kitty-keyboard capability probing, and a dirty-cell diff engine so only changed glyphs are written to the TTY.

## Architecture

The engine is split into five layers:

```
+-----------------------------------------------------+
|  Application                                        |
|  (event loop → set cells → Render)                  |
+-----------------------------------------------------+
         |                        |
         v                        v
+-------------------+   +-----------------------------+
| xui (engine)      |   | screen                      |
|  - XUI struct     |   |  - double-buffered cell grid|
|  - Loop            |   |  - Diff → DirtyCell         |
|  - caps management |   |  - CJK trail tracking       |
+-------------------+   +-----------------------------+
         |                        |
         v                        v
+-------------------+   +-----------------------------+
| render            |   | input                       |
|  - ANSI encoder   |   |  - byte stream parser       |
|  - SGR / cursor   |   |  - Key / Mouse / Paste      |
|  - Sync / alt      |   |  - CapEvent decoder         |
+-------------------+   +-----------------------------+
         |                        |
         v                        v
+-----------------------------------------------------+
| term (TTY abstraction)                              |
|  - Read / Write / Size / Raw mode                   |
|  - Unix / BSD / Linux / Windows                     |
+-----------------------------------------------------+
```

| Package | Responsibility |
|---------|---------------|
| `cell` | Primitive cell data: glyph, width, style, hyperlink, trail flag. |
| `term` | Low-level terminal I/O abstraction and platform raw-mode handling. |
| `input` | Byte-stream → structured `Event` (key, mouse, paste, focus, resize, capability probe). |
| `screen` | Double-buffered grid, diff engine, and cursor state. |
| `render` | Dirty cells → ANSI escape sequences (SGR, cursor motion, alt-screen, sync). |
| `xui` | Engine coordination: open TTY, probe capabilities, run the event loop, render frames. |

## Data Flow

```
┌──────────────────────────────────────────────────────────────┐
│                        Terminal (TTY)                        │
│  raw bytes from keyboard, mouse, resize, capability replies  │
└──────────────────────────┬───────────────────────────────────┘
                           │ term.TTY.Read()
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  input.Parser.Feed(buf)                                      │
│  - Decodes CSI / OSC / X10 / SGR mouse / bracketed paste     │
│  - Emits Event values (KeyEvent, MouseEvent, PasteEvent, …)  │
└──────────────────────────┬───────────────────────────────────┘
                           │ Loop.handle(ev)
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  Loop                                                        │
│  - Reads on a background goroutine                           │
│  - CapEvent → vx.applyCap (updates terminal capabilities)    │
│  - Other events → events channel (NextEvent / TryEvent)      │
└──────────────────────────┬───────────────────────────────────┘
                           │ application calls:
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  screen.SetCell(x, y, cell)                                  │
│  - Writes into the back buffer                               │
│  - Fills CJK continuation columns as trail cells             │
└──────────────────────────┬───────────────────────────────────┘
                           │ application calls:
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  XUI.Render()                                                │
│    screen.Diff()  ──►  []cell.DirtyCell                      │
│    renderer.RenderDiff(w, dirty, cursor, …)                  │
│      - Encodes changed cells as ANSI sequences               │
│      - Skips trail pads to avoid CJK erasure bugs            │
│      - Writes synchronized frame to term.TTY                 │
└──────────────────────────────────────────────────────────────┘
```

### Step-by-step walkthrough

1. **Probe.** `XUI.QueryTerminal()` sends a burst of capability sequences (Kitty keyboard, DECRQM, DA1, XTVersion) and waits for the terminal to reply.
2. **Read.** `Loop.readLoop` continuously reads from the TTY and feeds bytes into `input.Parser`.
3. **Parse.** The parser recognizes escape sequences and converts them into typed events. `CapEvent` responses update `render.Caps` in place; everything else is posted to the application channel.
4. **Draw.** The application calls `screen.SetCell()` to stage content into the back buffer.
5. **Diff.** `screen.Diff()` compares the back buffer against the front buffer and emits only the rows or cells that actually changed.
6. **Render.** `renderer.RenderDiff()` encodes the dirty cells into ANSI, moves the cursor, applies style deltas, and writes the complete frame to the TTY.
7. **Present.** After a successful write, `screen.Present()` swaps the front and back buffers so the next diff starts from the new ground truth.



## License

Apache License 2.0 — see [LICENSE](LICENSE).
