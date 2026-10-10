# Game controllers

Kwakore implements the [NAP-GAMEPAD draft in naps PR #108](https://github.com/napplet/naps/pull/108), pinned to commit `0b28f77e3dfa0f07dd7c0f9d61a7090ee99f3a5e`.

A napplet can read controllers synchronously during its animation loop:

```js
function frame() {
  const pads = window.napplet.gamepad.getGamepads();
  const player = pads.find(pad => pad && pad.connected);
  if (player) {
    movePlayer(player.axes[0], player.axes[1]);
    if (player.buttons[0]?.pressed) jump();
  }
  requestAnimationFrame(frame);
}
requestAnimationFrame(frame);

const subscription = window.napplet.gamepad.onChange(state => {
  // state has available, focused, pads, and an optional reason.
  showControllerStatus(state.available, state.focused);
});
// When the listener is no longer needed:
// subscription.close();
```

The binding caches snapshots locally; reads do not issue RPCs. The daemon owns one Linux controller monitor shared by all napplet windows. Only the visible, focused napplet receives live input. Background windows receive connection metadata with neutral axes, buttons and timestamps. Opening a Kwakore permission prompt also neutralizes input immediately. Reloading or closing a window removes its subscription, and closing the last subscriber stops the monitor.

Games using `navigator.getGamepads()`, `gamepadconnected` and `gamepaddisconnected` can use the same focus-controlled snapshots through the compatibility binding. Kwakore blocks direct controller access inside the napplet iframe. If the browser cannot enforce that policy, Kwakore refuses to run the napplet.

Linux input comes from SDL2 (`libSDL2-2.0.so.0`). Install your distribution's SDL2 runtime package if it is missing (for example `libsdl2-2.0-0` on Debian/Ubuntu); Nix packaging includes it. The daemon user needs read access to the controller's `/dev/input/event*` device. Missing libraries report `available: false` and reason `unavailable`; an accessible monitor with no readable controllers reports an empty controller view.

Known controllers use SDL's mappings and expose `mapping: "standard"`. Unmapped controllers expose a raw layout with an empty mapping. Kwakore limits snapshots to eight slots, sixteen axes and thirty-two buttons per controller. Disconnected slots are null. Standard trigger buttons expose analog travel; other buttons are digital. Haptics are not implemented.

## Verification

Run the backend and desktop suites as described in [service.md](service.md). The browser integration tests require a live X11 display and `xdotool`, or Xvfb:

```sh
just webview-libs
cd desktop
NO_AT_BRIDGE=1 KWAKORE_WEBKIT_SMOKE=1 xvfb-run -a go test ./child -run '^TestWebKit' -count=1 -v
```

An additional test creates and destroys a temporary virtual Xbox controller. It requires read access to evdev devices and write access to `/dev/uinput`, alongside Xvfb and `xdotool`:

```sh
cd desktop
NO_AT_BRIDGE=1 KWAKORE_WEBKIT_SMOKE=1 KWAKORE_GAMEPAD_UINPUT=1 xvfb-run -a go test ./child -run '^TestGamepadVirtualDevice$' -count=1 -v
```

This checks native discovery, standard mapping, button press/release, analog axes and triggers, D-pad input, focus changes between two windows, background neutralization and disconnect delivery to both windows.

For physical acceptance, open a game napplet, focus its content and exercise every control. Open a second game, switch focus while holding a button, and verify that only the focused game responds. Open a permission prompt and unplug/reconnect the controller. Device-specific mappings and an actual game's frame loop still need this physical play check.
