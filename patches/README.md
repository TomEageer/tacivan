# Patches

Tacivan builds against an unmodified upstream Wails v2.16.0 plus the diff in this
directory, applied on top of `go mod vendor`:

| File | What it changes |
|---|---|
| `wails-v2.16.0-macos-native-menu.diff` | macOS only. (1) The role menus Wails hard-codes in Objective-C ("Edit", "Window", "About …", "Quit …") are looked up through `NSBundle` so they follow the app's language. (2) The application menu gains a HIG-standard **Settings…** (⌘,) item that raises a `menu:settings` event. (3) Double-clicking the title bar honours the system `AppleActionOnDoubleClick` preference. |

`scripts/vendor.sh` runs `go mod vendor` and applies the patch; `vendor/` itself is not
committed. Wails is MIT licensed; see `vendor/github.com/wailsapp/wails/v2/LICENSE`
after vendoring.
